package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T) []Agent {
	b, err := os.ReadFile("testdata/ls.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := parseLS(b, "/tmp/kitty-1")
	if err != nil {
		t.Fatal(err)
	}
	sortAgents(a)
	return a
}

func TestParseFilterAndOrder(t *testing.T) {
	a := fixture(t)
	var got []string
	for _, x := range a {
		got = append(got, x.State+":"+x.Project+":"+x.Agent)
	}
	want := "blocked:proj:claude idle:-:codex working:-:claude"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestProject(t *testing.T) {
	for in, want := range map[string]string{"": "-", "/x/proj.kitty-session": "proj", "plain": "plain"} {
		if got := project(in); got != want {
			t.Errorf("project(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSwiftbar(t *testing.T) {
	var b bytes.Buffer
	renderSwiftbar(&b, fixture(t), "/bin/beckon")
	out := b.String()
	for _, s := range []string{"🔴 1| color=#f38ba8", "Waiting for your input (1)", "Idle (1)", "Working (1)", "param1=focus param2=/tmp/kitty-1 param3=11"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	b.Reset()
	renderSwiftbar(&b, nil, "x")
	if !strings.HasPrefix(b.String(), "●|") {
		t.Errorf("empty output: %q", b.String())
	}
}

func TestMergeHooksIdempotent(t *testing.T) {
	tg := targets()[0]
	in := []byte(`{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"other"}]}]}}`)
	once, added, err := mergeHooks(in, tg)
	if err != nil || len(added) != 5 {
		t.Fatalf("added=%v err=%v", added, err)
	}
	twice, added, _ := mergeHooks(once, tg)
	if len(added) != 0 || !bytes.Equal(once, twice) {
		t.Fatalf("not idempotent: added=%v", added)
	}
	for _, s := range []string{`"model": "opus"`, `"command": "other"`, "beckon hook idle", `"matcher": "*"`} {
		if !strings.Contains(string(once), s) {
			t.Errorf("lost %q", s)
		}
	}
}
