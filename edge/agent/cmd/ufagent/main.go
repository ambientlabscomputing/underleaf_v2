package main

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/cli"
)

func main() {
	o := cli.AgentCLI{}
	o.Execute()
}
