package agent

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	"github.com/spf13/cobra"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	maistrainformer "maistra.io/api/client/informers/externalversions"
	maistraclientset "maistra.io/api/client/versioned"
	"open-cluster-management.io/addon-framework/pkg/lease"
	"open-cluster-management.io/addon-framework/pkg/version"

	meshclientset "github.com/stolostron/multicluster-mesh-addon/apis/client/clientset/versioned"
	meshinformer "github.com/stolostron/multicluster-mesh-addon/apis/client/informers/externalversions"
	meshdeploy "github.com/stolostron/multicluster-mesh-addon/pkg/agent/deploy"
	meshdiscovery "github.com/stolostron/multicluster-mesh-addon/pkg/agent/discovery"
	meshfederation "github.com/stolostron/multicluster-mesh-addon/pkg/agent/federation"
)

func NewAgentCommand(addonName string) *cobra.Command {
	o := NewAgentOptions(addonName)
	cmd := controllercmd.
		NewControllerCommandConfig("multicluster-mesh-addon-agent", version.Get(), o.RunAgent).
		NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the multicluster mesh addon agent"

	o.AddFlags(cmd)
	return cmd
}

// AgentOptions defines the flags for workload agent
type AgentOptions struct {
	HubKubeconfigFile string
	SpokeClusterName  string
	AddonName         string
	AddonNamespace    string
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewAgentOptions(addonName string) *AgentOptions {
	return &AgentOptions{AddonName: addonName}
}

func (o *AgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
	flags.StringVar(&o.AddonNamespace, "addon-namespace", o.AddonNamespace, "Installation namespace of addon.")
}

// RunAgent starts the controllers on agent to process work from hub.
func (o *AgentOptions) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// build kubeclient of managed cluster
	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// build olm client of managed cluster
	spokeOLMClient, err := olmclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// build maistraClient of managed cluster
	spokeMaistraClient, err := maistraclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// build spoke kube informer factory
	spokeKubeInformerFactory := informers.NewSharedInformerFactory(spokeKubeClient, 10*time.Minute)

	// build spoke maistra informer factory
	spokeMaistraInformerFactory := maistrainformer.NewSharedInformerFactory(spokeMaistraClient, 10*time.Minute)

	// build kubeconfig of hub cluster
	hubRestConfig, err := clientcmd.BuildConfigFromFlags("", o.HubKubeconfigFile)
	if err != nil {
		return err
	}

	// build kube client of hub cluster
	hubKubeClient, err := kubernetes.NewForConfig(hubRestConfig)

	// build hub kube informer factory
	hubKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute, informers.WithNamespace(o.SpokeClusterName))

	// build meshClient of hub cluster
	hubMeshClient, err := meshclientset.NewForConfig(hubRestConfig)
	if err != nil {
		return err
	}

	// build hub mesh informer factory
	hubMeshInformerFactory := meshinformer.NewSharedInformerFactoryWithOptions(hubMeshClient, 10*time.Minute, meshinformer.WithNamespace(o.SpokeClusterName))

	// create an mesh-discovery controller
	discoveryController := meshdiscovery.NewDiscoveryController(
		o.SpokeClusterName,
		o.AddonNamespace,
		hubMeshClient,
		spokeMaistraInformerFactory.Core().V2().ServiceMeshControlPlanes(),
		spokeMaistraInformerFactory.Core().V1().ServiceMeshMemberRolls(),
		controllerContext.EventRecorder,
	)

	// create an mesh-deploy controller
	deployController := meshdeploy.NewDeployController(
		o.SpokeClusterName,
		o.AddonNamespace,
		hubMeshClient,
		hubMeshInformerFactory.Mesh().V1alpha1().Meshes(),
		spokeKubeClient,
		spokeOLMClient,
		spokeMaistraClient,
		controllerContext.EventRecorder,
	)

	// create an mesh-federation controller
	federationController := meshfederation.NewFederationController(
		o.SpokeClusterName,
		o.AddonNamespace,
		hubKubeClient,
		spokeKubeClient,
		spokeMaistraClient,
		hubKubeInformerFactory.Core().V1().ConfigMaps(),
		spokeKubeInformerFactory.Core().V1().Services(),
		spokeKubeInformerFactory.Core().V1().ConfigMaps(),
		controllerContext.EventRecorder,
	)

	// create a lease updater
	leaseUpdater := lease.NewLeaseUpdater(
		spokeKubeClient,
		o.AddonName,
		o.AddonNamespace,
	)

	go hubKubeInformerFactory.Start(ctx.Done())
	go spokeKubeInformerFactory.Start(ctx.Done())
	go spokeMaistraInformerFactory.Start(ctx.Done())
	go hubMeshInformerFactory.Start(ctx.Done())
	go discoveryController.Run(ctx, 1)
	go deployController.Run(ctx, 1)
	go federationController.Run(ctx, 1)
	go leaseUpdater.Start(ctx)

	<-ctx.Done()
	return nil
}
