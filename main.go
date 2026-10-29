// beckon: know which AI agent is calling you (kitty + macOS menu bar).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
)

const usage = `beckon: know which AI agent is calling you.

usage:
  beckon ls [blocked|idle|working] [--json]   list agents running in kitty
  beckon focus <socket> <window-id>           focus an agent's window
  beckon hook <working|blocked|idle|release>  agent hook: publish state (reads hook JSON on stdin)
  beckon swiftbar                             SwiftBar plugin output
  beckon install [claude|codex|swiftbar] [--dry-run]
  beckon doctor                               check what beckon can see
  beckon version
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"ls"}
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "ls":
		cmdLs(rest)
	case "focus":
		if len(rest) != 2 {
			die(usage)
		}
		id, err := strconv.Atoi(rest[1])
		if err != nil {
			die("bad window id: " + rest[1])
		}
		if err := focus(rest[0], id); err != nil {
			die(err.Error())
		}
	case "hook":
		in, _ := io.ReadAll(os.Stdin)
		if len(rest) > 0 {
			hook(rest[0], in)
		}
	case "swiftbar":
		runSwiftbar(os.Stdout)
	case "install":
		os.Exit(cmdInstall(rest))
	case "doctor":
		os.Exit(doctor())
	case "version":
		v := "devel"
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
			v = bi.Main.Version
		}
		fmt.Println("beckon", v, runtime.GOOS+"/"+runtime.GOARCH)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		die(usage)
	}
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func cmdLs(args []string) {
	var state string
	asJSON := false
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		default:
			state = a
		}
	}
	all, err := listAgents()
	if err != nil {
		die(err.Error())
	}
	out := []Agent{}
	for _, a := range all {
		if state == "" || a.State == state {
			out = append(out, a)
		}
	}
	if asJSON {
		json.NewEncoder(os.Stdout).Encode(out)
		return
	}
	printTable(os.Stdout, out)
}
