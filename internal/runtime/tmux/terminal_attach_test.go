package tmux

import (
	"strings"
	"testing"
)

func TestProviderTerminalAttachCommandUsesScriptPTYAndSocket(t *testing.T) {
	p := NewProviderWithConfig(Config{SocketName: "city-socket"})

	spec, err := p.TerminalAttachCommand("mayor")
	if err != nil {
		t.Fatalf("TerminalAttachCommand: %v", err)
	}

	if spec.Path != "script" {
		t.Fatalf("Path = %q, want script", spec.Path)
	}
	if len(spec.Args) != 3 || spec.Args[0] != "-qfc" || spec.Args[2] != "/dev/null" {
		t.Fatalf("Args = %#v, want script -qfc <cmd> /dev/null", spec.Args)
	}
	for _, want := range []string{"tmux", "-u", "-L", "city-socket", "attach-session", "-t", "mayor"} {
		if !strings.Contains(spec.Args[1], want) {
			t.Fatalf("attach command = %q, want %q", spec.Args[1], want)
		}
	}
	if len(spec.Env) != 1 || spec.Env[0] != "TERM=xterm-256color" {
		t.Fatalf("Env = %#v, want TERM=xterm-256color", spec.Env)
	}
}

func TestProviderTerminalResizeCommandUsesSocketAndDimensions(t *testing.T) {
	p := NewProviderWithConfig(Config{SocketName: "city-socket"})

	spec, err := p.TerminalResizeCommand("mayor", 120, 37)
	if err != nil {
		t.Fatalf("TerminalResizeCommand: %v", err)
	}

	if spec.Path != "tmux" {
		t.Fatalf("Path = %q, want tmux", spec.Path)
	}
	want := []string{"-u", "-L", "city-socket", "resize-window", "-t", "mayor", "-x", "120", "-y", "37"}
	if strings.Join(spec.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("Args = %#v, want %#v", spec.Args, want)
	}
}
