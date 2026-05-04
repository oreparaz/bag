package main

import (
	"os"
	"testing"
)

func TestDispatchByArgv0(t *testing.T) {
	cases := []struct {
		argv0 string
		want  bool // true if should dispatch to a tool
	}{
		{"curl", true},
		{"wget", true},
		{"bag-curl", true},
		{"/usr/bin/curl", true},
		{"./bag", false}, // dispatched as multicall, not direct tool
	}
	for _, c := range cases {
		// We can't easily call dispatch() with side-effecting tools, but we
		// can verify name resolution by inspecting the tools table.
		all := tools()
		byName := map[string]bool{}
		for _, t := range all {
			byName[t.Name] = true
		}
		base := stripPath(c.argv0)
		base = stripBagPrefix(base)
		got := byName[base]
		if got != c.want {
			t.Errorf("argv0=%q -> tool=%v, want %v", c.argv0, got, c.want)
		}
	}
}

func stripPath(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

func stripBagPrefix(p string) string {
	const prefix = "bag-"
	if len(p) > len(prefix) && p[:len(prefix)] == prefix {
		return p[len(prefix):]
	}
	return p
}

func TestListAndVersion(t *testing.T) {
	if dispatch([]string{"bag", "--list"}) != 0 {
		t.Errorf("--list should exit 0")
	}
	if dispatch([]string{"bag", "--version"}) != 0 {
		t.Errorf("--version should exit 0")
	}
	if dispatch([]string{"bag"}) == 0 {
		t.Errorf("no args should exit non-zero")
	}
	if dispatch([]string{"bag", "no-such-tool"}) == 0 {
		t.Errorf("unknown tool should exit non-zero")
	}
	if testing.Verbose() {
		_, _ = os.Stdout.Write([]byte("ok\n"))
	}
}
