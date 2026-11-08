package cli

import (
	"errors"
	"reflect"
	"testing"
)

func fake(t *testing.T) (dockerRunner, *[]string) {
	t.Helper()
	var ran []string
	return dockerRunner{
		lookPath:   func(string) (string, error) { return "/usr/bin/docker", nil },
		socketGone: func(string) bool { return true },
		running:    func(string, string) bool { return true },
		interact:   func() bool { return false },
		run:        func(_ string, a []string) int { ran = a; return 7 },
	}, &ran
}

func TestDelegatesIntoTheContainer(t *testing.T) {
	t.Setenv(EnvNoDockerDelegate, "")
	t.Setenv("UNDERLEAF_ORCH_SOCKET", "")
	r, ran := fake(t)
	code, ok := delegateToContainer([]string{"nodes", "add", "--advertise", "h:50104"}, r)
	if !ok || code != 7 {
		t.Fatalf("ok=%v code=%d, want delegated with the container's exit code", ok, code)
	}
	want := []string{"exec", "-i", "-e", "UNDERLEAF_NO_DOCKER_DELEGATE=1", "underleaf", "orcli", "nodes", "add", "--advertise", "h:50104"}
	if !reflect.DeepEqual(*ran, want) {
		t.Fatalf("docker args = %v, want %v", *ran, want)
	}
}

func TestAllocatesATTYOnlyForInteractiveUse(t *testing.T) {
	t.Setenv(EnvNoDockerDelegate, "")
	t.Setenv("UNDERLEAF_ORCH_SOCKET", "")
	r, ran := fake(t)
	r.interact = func() bool { return true }
	delegateToContainer([]string{"nodes", "ls"}, r)
	if (*ran)[2] != "-t" {
		t.Fatalf("expected -t, got %v", *ran)
	}
}

func TestCustomContainerName(t *testing.T) {
	t.Setenv(EnvNoDockerDelegate, "")
	t.Setenv("UNDERLEAF_ORCH_SOCKET", "")
	t.Setenv(EnvOrchContainer, "mine")
	r, ran := fake(t)
	delegateToContainer([]string{"nodes", "ls"}, r)
	if (*ran)[4] != "mine" {
		t.Fatalf("container not honoured: %v", *ran)
	}
}

func TestDoesNotDelegate(t *testing.T) {
	cases := map[string]struct {
		args []string
		env  map[string]string
		mod  func(*dockerRunner)
	}{
		"no args":               {args: nil},
		"run starts a server":   {args: []string{"run"}},
		"help":                  {args: []string{"help"}},
		"completion":            {args: []string{"completion", "zsh"}},
		"bare flag":             {args: []string{"--help"}},
		"disabled":              {args: []string{"nodes", "ls"}, env: map[string]string{EnvNoDockerDelegate: "1"}},
		"explicit socket":       {args: []string{"nodes", "ls"}, env: map[string]string{"UNDERLEAF_ORCH_SOCKET": "/x.sock"}},
		"local socket exists":   {args: []string{"nodes", "ls"}, mod: func(r *dockerRunner) { r.socketGone = func(string) bool { return false } }},
		"docker not installed":  {args: []string{"nodes", "ls"}, mod: func(r *dockerRunner) { r.lookPath = func(string) (string, error) { return "", errors.New("no") } }},
		"container not running": {args: []string{"nodes", "ls"}, mod: func(r *dockerRunner) { r.running = func(string, string) bool { return false } }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(EnvNoDockerDelegate, "")
			t.Setenv("UNDERLEAF_ORCH_SOCKET", "")
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			r, ran := fake(t)
			if c.mod != nil {
				c.mod(&r)
			}
			if _, ok := delegateToContainer(c.args, r); ok || *ran != nil {
				t.Fatalf("delegated (%v) when it should not have", *ran)
			}
		})
	}
}
