package main

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli"
)

func main() {
	o := cli.OrchestratorCLI{}
	o.Execute()
}
