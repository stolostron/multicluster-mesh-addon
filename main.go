package main

import (
	"context"
	goflag "flag"
	"fmt"
	"os"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	utilflag "k8s.io/component-base/cli/flag"
	logs "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	meshcontroller "github.com/stolostron/multicluster-mesh-addon/pkg/hub/mesh"
	"github.com/stolostron/multicluster-mesh-addon/pkg/version"
)

var (
	runtimeScheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(scheme.AddToScheme(runtimeScheme))
	utilruntime.Must(meshv1alpha1.Install(runtimeScheme))
	utilruntime.Must(clusterv1.Install(runtimeScheme))
	utilruntime.Must(clusterv1beta2.Install(runtimeScheme))
	utilruntime.Must(workv1.Install(runtimeScheme))
	utilruntime.Must(operatorsv1.AddToScheme(runtimeScheme))
	utilruntime.Must(operatorsv1alpha1.AddToScheme(runtimeScheme))
}

func main() {
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.AddFlags(logs.NewLoggingConfiguration(), pflag.CommandLine)

	command := newCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "multicluster-mesh-addon",
		Short: "Multi Cluster Mesh Add On",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
			os.Exit(1)
		},
	}

	if v := version.Get().String(); len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(newControllerCommand())

	return cmd
}

var (
	metricsAddr string
	probeAddr   string
)

func newControllerCommand() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("multicluster-mesh-addon-controller", version.Get(), runController, clock.RealClock{}).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the addon controller"

	cmd.Flags().StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-addr", ":8081", "The address the probe endpoint binds to.")

	return cmd
}

func runController(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	klog.Info("Starting Multi Cluster Mesh Add On controller...")

	// Create controller-runtime manager
	// Leader election is handled by library-go controllercmd (enabled by default)
	mgr, err := manager.New(controllerContext.KubeConfig, manager.Options{
		Scheme: runtimeScheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
	})
	if err != nil {
		klog.Errorf("Unable to set up controller manager: %v", err)
		return err
	}

	// Register MultiClusterMesh controller
	if err := meshcontroller.RegisterController(mgr); err != nil {
		klog.Errorf("Unable to register MultiClusterMesh controller: %v", err)
		return err
	}

	klog.Info("Starting manager...")
	if err := mgr.Start(ctx); err != nil {
		klog.Errorf("Problem running manager: %v", err)
		return err
	}

	return nil
}
