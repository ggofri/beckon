package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Agent is one kitty window whose foreground process is a known agent and
// whose `agent` user var holds a live state.
type Agent struct {
	State   string `json:"state"`
	Project string `json:"project"`
	Tab     string `json:"tab"`
	Agent   string `json:"agent"`
	ID      int    `json:"id"`
	Socket  string `json:"socket"`
}

var (
	agentRe   = regexp.MustCompile(`claude|codex|opencode`)
	stateRank = map[string]int{"blocked": 0, "idle": 1, "working": 2}
)

type lsOS []struct {
	Tabs []struct {
		Title       string `json:"title"`
		SessionName string `json:"session_name"`
		Windows     []struct {
			ID          int               `json:"id"`
			SessionName *string           `json:"session_name"`
			UserVars    map[string]string `json:"user_vars"`
			Foreground  []struct {
				Cmdline []string `json:"cmdline"`
			} `json:"foreground_processes"`
		} `json:"windows"`
	} `json:"tabs"`
}

func findKitten() (string, error) {
	if p, err := exec.LookPath("kitten"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		"/Applications/kitty.app/Contents/MacOS/kitten",
		filepath.Join(home, "Applications/kitty.app/Contents/MacOS/kitten"),
		filepath.Join(home, ".local/kitty.app/bin/kitten"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("kitten not found (install kitty: https://sw.kovidgoyal.net/kitty)")
}

func project(session string) string {
	parts := strings.Split(session, "/")
	p := strings.TrimSuffix(parts[len(parts)-1], ".kitty-session")
	if p == "" {
		return "-"
	}
	return p
}

// parseLS turns `kitten @ ls` JSON into agents; stale states (the agent exited
// and the window is back to a shell) are dropped by the foreground-process check.
func parseLS(data []byte, sock string) ([]Agent, error) {
	var os_ lsOS
	if err := json.Unmarshal(data, &os_); err != nil {
		return nil, err
	}
	var out []Agent
	for _, o := range os_ {
		for _, t := range o.Tabs {
			for _, w := range t.Windows {
				st := w.UserVars["agent"]
				if _, ok := stateRank[st]; !ok {
					continue
				}
				var cmds []string
				for _, f := range w.Foreground {
					cmds = append(cmds, strings.Join(f.Cmdline, " "))
				}
				name := agentRe.FindString(strings.Join(cmds, " "))
				if name == "" {
					continue
				}
				sess := t.SessionName
				if w.SessionName != nil {
					sess = *w.SessionName
				}
				out = append(out, Agent{st, project(sess), t.Title, name, w.ID, sock})
			}
		}
	}
	return out, nil
}

func sortAgents(a []Agent) {
	sort.SliceStable(a, func(i, j int) bool {
		x, y := a[i], a[j]
		if x.State != y.State {
			return stateRank[x.State] < stateRank[y.State]
		}
		if x.Project != y.Project {
			return x.Project < y.Project
		}
		return x.Tab < y.Tab
	})
}

func isSocket(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode()&os.ModeSocket != 0
}

func kittyConfDir() string {
	if d := os.Getenv("KITTY_CONFIG_DIRECTORY"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config/kitty")
}

// confListenOn returns the `listen_on` value from kitty.conf ("" if absent).
// ponytail: does not follow `include` lines.
func confListenOn() string {
	b, err := os.ReadFile(filepath.Join(kittyConfDir(), "kitty.conf"))
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "listen_on" {
			return f[1]
		}
	}
	return ""
}

// findSockets unions $KITTY_LISTEN_ON, kitty.conf's listen_on and /tmp/kitty-*.
// ponytail: unix socket paths only, no Linux abstract sockets (unix:@name).
func findSockets() []string {
	var cands []string
	add := func(spec string) {
		if p, ok := strings.CutPrefix(spec, "unix:"); ok && !strings.HasPrefix(p, "@") {
			cands = append(cands, strings.ReplaceAll(p, "{kitty_pid}", "*"))
		}
	}
	add(os.Getenv("KITTY_LISTEN_ON"))
	add(confListenOn())
	cands = append(cands, "/tmp/kitty-*")
	seen := map[string]bool{}
	var out []string
	for _, c := range cands {
		m, _ := filepath.Glob(c)
		for _, p := range m {
			if !seen[p] && isSocket(p) {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func kittenSock(sock string, args ...string) ([]byte, error) {
	k, err := findKitten()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return exec.CommandContext(ctx, k, append([]string{"@", "--to", "unix:" + sock}, args...)...).Output()
}

// listAgents queries every socket in parallel; dead sockets are skipped.
func listAgents() ([]Agent, error) {
	if _, err := findKitten(); err != nil {
		return nil, err
	}
	socks := findSockets()
	res := make([][]Agent, len(socks))
	var wg sync.WaitGroup
	for i, s := range socks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b, err := kittenSock(s, "ls"); err == nil {
				res[i], _ = parseLS(b, s)
			}
		}()
	}
	wg.Wait()
	var all []Agent
	for _, r := range res {
		all = append(all, r...)
	}
	sortAgents(all)
	return all, nil
}

func focus(sock string, id int) error {
	sock = strings.TrimPrefix(sock, "unix:")
	if _, err := kittenSock(sock, "focus-window", "--match", "id:"+strconv.Itoa(id)); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-a", "kitty").Run()
	}
	return nil
}

var stateColor = map[string]string{"working": "#f9e2af", "blocked": "#f38ba8", "idle": "#a6e3a1", "release": "NONE"}

// hook paints the tab and publishes the state; it never fails (it runs inside agent hooks).
func hook(state string, stdin []byte) {
	bg, ok := stateColor[state]
	if !ok {
		return
	}
	if strings.Contains(string(stdin), `"agent_id"`) && (state == "idle" || state == "release") {
		return // subagent finished; the main agent is still running
	}
	win := os.Getenv("KITTY_WINDOW_ID")
	if win == "" || os.Getenv("KITTY_LISTEN_ON") == "" {
		return
	}
	k, err := findKitten()
	if err != nil {
		return
	}
	fg, v := "#11111b", state
	if state == "release" {
		fg, v = "NONE", ""
	}
	exec.Command(k, "@", "set-tab-color", "--match", "window_id:"+win, "inactive_bg="+bg, "inactive_fg="+fg).Run()
	exec.Command(k, "@", "set-user-vars", "--match", "id:"+win, "agent="+v).Run()
}

func printTable(w io.Writer, a []Agent) {
	for _, x := range a {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", x.State, x.Project, x.Tab, x.Agent, x.ID, x.Socket)
	}
}
