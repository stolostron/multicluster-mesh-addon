package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"
	"k8s.io/component-base/logs"
	_ "k8s.io/component-base/logs/json/register"
	"open-cluster-management.io/addon-framework/pkg/version"

	meshagent "github.com/stolostron/multicluster-mesh-addon/pkg/agent"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
	meshmanager "github.com/stolostron/multicluster-mesh-addon/pkg/manager"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	logsOptions := logs.NewOptions()
	command := newCommand(logsOptions)
	logsOptions.AddFlags(command.Flags())
	code := cli.Run(command)
	os.Exit(code)
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newCommand(logsOptions *logs.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addon",
		Short: "multicluster mesh addon",
		Run: func(cmd *cobra.Command, args []string) {
			if err := logsOptions.ValidateAndApply(); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
		},
	}

	if v := version.Get().String(); len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(meshmanager.NewControllerCommand())
	cmd.AddCommand(meshagent.NewAgentCommand(constants.MeshAddonName))

	return cmd
}
