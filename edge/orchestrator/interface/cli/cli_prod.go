//go:build !devmode

package cli

import "github.com/spf13/cobra"

func registerDevCommands(_ *cobra.Command) {}
