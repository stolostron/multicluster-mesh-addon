package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/stolostron/multicluster-mesh-addon/pkg/version"
)

func main() {
	command := newCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "meshctl",
		Short:         "Tools for working with MultiClusterMesh deployments",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.Version = version.Get().String()
	command.AddCommand(newScaffoldCommand())

	return command
}
