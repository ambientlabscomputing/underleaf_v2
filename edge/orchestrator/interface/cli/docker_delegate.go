package cli

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	sharedutils "github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// The Orchestrator installed by install.sh runs in a Docker container, and
// orcli talks to it over a unix socket that exists only inside that container,
// so a host orcli has nothing to dial. Rather than make users type `docker exec`,
// orcli notices this situation and runs the same command inside the container
// for them. The trust model is unchanged: it needs access to the Docker daemon,
// which already means root on the machine, and it exposes nothing new to the
// network (the socket stays unpublished).
const (
	// EnvOrchContainer names the Orchestrator container. Default "underleaf".
	EnvOrchContainer = "UNDERLEAF_ORCH_CONTAINER"
	// EnvNoDockerDelegate disables the fallback. It is also set inside the
	// container so the delegated command can never delegate again.
	EnvNoDockerDelegate = "UNDERLEAF_NO_DOCKER_DELEGATE"

	defaultOrchContainer = "underleaf"
)

// dockerRunner abstracts the pieces that touch the machine, for tests.
type dockerRunner struct {
	lookPath   func(string) (string, error)
	socketGone func(path string) bool
	running    func(docker, container string) bool
	interact   func() (stdinIsTTY bool)
	run        func(docker string, args []string) int
}

func realRunner() dockerRunner {
	return dockerRunner{
		lookPath: exec.LookPath,
		socketGone: func(path string) bool {
			_, err := os.Lstat(path)
			return errors.Is(err, os.ErrNotExist)
		},
		running: func(docker, container string) bool {
			out, err := exec.Command(docker, "inspect", "-f", "{{.State.Running}}", container).Output()
			return err == nil && strings.TrimSpace(string(out)) == "true"
		},
		interact: func() bool {
			fi, err := os.Stdin.Stat()
			if err != nil {
				return false
			}
			out, err := os.Stdout.Stat()
			return err == nil && fi.Mode()&os.ModeCharDevice != 0 && out.Mode()&os.ModeCharDevice != 0
		},
		run: func(docker string, args []string) int {
			cmd := exec.Command(docker, args...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			// Docker prints a "try Docker Debug" hint after an interactive exec.
			cmd.Env = append(os.Environ(), "DOCKER_CLI_HINTS=false")
			if err := cmd.Run(); err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					return ee.ExitCode()
				}
				os.Stderr.WriteString("orcli: could not run docker: " + err.Error() + "\n")
				return 1
			}
			return 0
		},
	}
}

// delegateToContainer runs args inside the Orchestrator container when that is
// where the Orchestrator is, and reports whether it did (with the exit code).
//
// It delegates only when ALL hold: it is not disabled; the command talks to a
// running Orchestrator (not `run`, `help`, `completion` or a bare flag); the
// user did not pick a socket explicitly; the default socket does not exist on
// this machine (a locally running Orchestrator always wins); docker is
// installed; and the container is running.
func delegateToContainer(args []string, r dockerRunner) (int, bool) {
	if os.Getenv(EnvNoDockerDelegate) != "" || len(args) == 0 {
		return 0, false
	}
	switch first := args[0]; {
	case strings.HasPrefix(first, "-"), first == "run", first == "help", first == "completion":
		return 0, false
	}
	if os.Getenv(sharedutils.EnvOrchSocket) != "" || !r.socketGone(sharedutils.OrchestratorSocketPath()) {
		return 0, false
	}
	docker, err := r.lookPath("docker")
	if err != nil {
		return 0, false
	}
	container := os.Getenv(EnvOrchContainer)
	if container == "" {
		container = defaultOrchContainer
	}
	if !r.running(docker, container) {
		return 0, false
	}
	dargs := []string{"exec", "-i"}
	if r.interact() {
		dargs = append(dargs, "-t")
	}
	dargs = append(dargs, "-e", EnvNoDockerDelegate+"=1", container, "orcli")
	dargs = append(dargs, args...)
	return r.run(docker, dargs), true
}
