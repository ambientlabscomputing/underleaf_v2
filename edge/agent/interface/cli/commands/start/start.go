package start

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/spf13/cobra"
)

var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the edge agent",
}

func RunBinary(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "GIN_MODE=release")

	stderr, err := cmd.StderrPipe()
	if err != nil {
		ui.PrintError("Failed to get stderr for %s: %v", name, err)
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		ui.PrintError("Failed to get stdout for %s: %v", name, err)
		return err
	}

	prefixStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Bold(true)
	lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#b9b9b9"))
	prefix := prefixStyle.Render("["+name+"]") + " "

	streamLines := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				fmt.Println(prefix + lineStyle.Render(line))
			}
		}
	}

	go streamLines(stderr)
	go streamLines(stdout)

	err = cmd.Run()
	if err != nil {
		ui.PrintError("Failed to run %s: %v", name, err)
	}
	return err
}

func init() {
	// --manager, bool: Whether to start orchestrator server (indicates this is a manager node)
	StartCmd.Flags().Bool("manager", false, "Whether to start orchestrator server (indicates this is a manager node)")
	StartCmd.RunE = func(cmd *cobra.Command, args []string) error {
		manager, _ := cmd.Flags().GetBool("manager")
		if manager {
			// Start both agent and orchestrator
			err := RunBinary("ufagent", "run")
			if err != nil {
				ui.PrintError("Failed to start agent: %v", err)
				return err
			}
			ui.Debug("starting orchestrator ...")
			RunBinary("orch-server", "run")
			ui.Printf("Orchestrator server started successfully")
		} else {
			// Start only the agent
			RunBinary("ufagentd", "run")
			ui.Printf("Agent started successfully")
		}
		return nil
	}
}
