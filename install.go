package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type target struct {
	name, path string
	matcher    bool // claude groups carry "matcher": "*"
	events     [][2]string
}

func targets() []target {
	home, _ := os.UserHomeDir()
	common := [][2]string{{"PermissionRequest", "blocked"}, {"PreToolUse", "working"}, {"UserPromptSubmit", "working"}, {"Stop", "idle"}}
	return []target{
		{"claude", filepath.Join(home, ".claude/settings.json"), true, append(common[:4:4], [2]string{"SessionEnd", "release"})},
		{"codex", filepath.Join(home, ".codex/hooks.json"), false, append(common[:4:4], [2]string{"SessionStart", "release"})},
	}
}

func hasBeckon(group any) bool {
	g, _ := group.(map[string]any)
	hs, _ := g["hooks"].([]any)
	for _, h := range hs {
		m, _ := h.(map[string]any)
		if c, _ := m["command"].(string); strings.Contains(c, "beckon hook") {
			return true
		}
	}
	return false
}

// mergeHooks adds beckon's hook groups to a claude/codex hooks config,
// keeping every other key. It reports the events it added.
// ponytail: re-marshalling sorts object keys; comments don't exist in JSON so nothing else is lost.
func mergeHooks(in []byte, t target) (out []byte, added []string, err error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(in)) > 0 {
		if err = json.Unmarshal(in, &root); err != nil {
			return nil, nil, err
		}
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, e := range t.events {
		groups, _ := hooks[e[0]].([]any)
		have := false
		for _, g := range groups {
			have = have || hasBeckon(g)
		}
		if have {
			continue
		}
		g := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "beckon hook " + e[1], "timeout": 10}}}
		if t.matcher {
			g["matcher"] = "*"
		}
		hooks[e[0]] = append(groups, g)
		added = append(added, e[0])
	}
	root["hooks"] = hooks
	out, err = json.MarshalIndent(root, "", "  ")
	return append(out, '\n'), added, err
}

func installHooks(t target, dry bool) error {
	in, err := os.ReadFile(t.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out, added, err := mergeHooks(in, t)
	if err != nil {
		return fmt.Errorf("%s: %w", t.path, err)
	}
	if len(added) == 0 {
		fmt.Printf("%s: already installed (%s)\n", t.name, t.path)
		return nil
	}
	if fi, err := os.Lstat(t.path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		fmt.Printf("%s: %s is a symlink, writing through it\n", t.name, t.path)
	}
	if dry {
		fmt.Printf("%s: would add %s to %s:\n%s", t.name, strings.Join(added, ", "), t.path, out)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(t.path, out, 0o644); err != nil { // follows symlinks
		return err
	}
	fmt.Printf("%s: added %s to %s\n", t.name, strings.Join(added, ", "), t.path)
	if t.name == "codex" {
		fmt.Println("codex: it may ask you to trust the new hooks on next start")
	}
	return nil
}

func cmdInstall(args []string) int {
	dry := false
	want := map[string]bool{}
	for _, a := range args {
		switch a {
		case "--dry-run":
			dry = true
		case "claude", "codex", "swiftbar":
			want[a] = true
		default:
			fmt.Fprintln(os.Stderr, "unknown argument:", a)
			return 2
		}
	}
	if len(want) == 0 {
		want["claude"], want["codex"], want["swiftbar"] = true, true, runtime.GOOS == "darwin"
	}
	rc := 0
	for _, t := range targets() {
		if want[t.name] {
			if err := installHooks(t, dry); err != nil {
				fmt.Fprintln(os.Stderr, err)
				rc = 1
			}
		}
	}
	if want["swiftbar"] {
		switch {
		case runtime.GOOS != "darwin":
			fmt.Fprintln(os.Stderr, "swiftbar: macOS only")
			rc = 1
		case dry:
			fmt.Println("swiftbar: would write beckon.3s.sh to the SwiftBar plugin directory")
		default:
			if err := installSwiftbar(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				rc = 1
			}
		}
	}
	return rc
}

func doctor() int {
	ok := func(good bool, msg string) bool {
		m := "✓"
		if !good {
			m = "✗"
		}
		fmt.Println(m, msg)
		return good
	}
	rc := 0
	k, err := findKitten()
	if !ok(err == nil, "kitten: "+orStr(k, "not found")) {
		rc = 1
	}
	socks := findSockets()
	alive := 0
	for _, s := range socks {
		if _, err := kittenSock(s, "ls"); err == nil {
			alive++
			ok(true, "socket "+s+" responds")
		} else {
			ok(false, "socket "+s+" does not respond (remote control disabled or kitty dead?)")
		}
	}
	if !ok(alive > 0, fmt.Sprintf("%d kitty socket(s) responding", alive)) {
		rc = 1
	}
	lo := confListenOn()
	switch {
	case strings.HasPrefix(lo, "unix:@"):
		ok(false, "listen_on is an abstract socket ("+lo+"); beckon needs a unix path")
	default:
		ok(lo != "", "kitty.conf listen_on: "+orStr(lo, "missing (add `listen_on unix:/tmp/kitty-{kitty_pid}`)"))
	}
	conf, _ := os.ReadFile(filepath.Join(kittyConfDir(), "kitty.conf"))
	ok(strings.Contains(string(conf), "allow_remote_control"), "kitty.conf allow_remote_control (use `socket-only` or `yes`)")
	for _, t := range targets() {
		b, _ := os.ReadFile(t.path)
		ok(strings.Contains(string(b), "beckon hook"), t.name+" hooks in "+t.path+" (fix: beckon install "+t.name+")")
	}
	if runtime.GOOS == "darwin" {
		_, err := os.Stat("/Applications/SwiftBar.app")
		ok(err == nil, "SwiftBar installed (brew install --cask swiftbar)")
		d := swiftbarDir()
		_, err = os.Stat(filepath.Join(d, "beckon.3s.sh"))
		_, err2 := os.Lstat(filepath.Join(d, "beckon.3s.sh"))
		ok(d != "" && (err == nil || err2 == nil), "SwiftBar plugin in "+orStr(d, "(PluginDirectory unset)")+" (fix: beckon install swiftbar)")
	}
	return rc
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
