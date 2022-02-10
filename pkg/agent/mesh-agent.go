package agent

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	maistrainformer "maistra.io/api/client/informers/externalversions"
	maistrav1informer "maistra.io/api/client/informers/externalversions/core/v1"
	maistrav2informer "maistra.io/api/client/informers/externalversions/core/v2"
	maistrav1lister "maistra.io/api/client/listers/core/v1"
	maistrav2lister "maistra.io/api/client/listers/core/v2"
	maistraclientset "maistra.io/api/client/versioned"
	"open-cluster-management.io/addon-framework/pkg/lease"
	"open-cluster-management.io/addon-framework/pkg/version"

	meshclientset "github.com/morvencao/multicluster-mesh-addon/apis/client/clientset/versioned"
	meshresourceapply "github.com/morvencao/multicluster-mesh-addon/pkg/resourceapply"
	meshtranslate "github.com/morvencao/multicluster-mesh-addon/pkg/translate"
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
	// build maistraClient of managed cluster
	spokeMaistraClient, err := maistraclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	spokeKubeInformerFactory := maistrainformer.NewSharedInformerFactory(spokeMaistraClient, 10*time.Minute)

	// build kubeconfig of hub cluster
	hubRestConfig, err := clientcmd.BuildConfigFromFlags("", o.HubKubeconfigFile)
	if err != nil {
		return err
	}
	// // build kubeclient of hub cluster
	// hubKubeClient, err := kubernetes.NewForConfig(hubRestConfig)
	// if err != nil {
	// 	return err
	// }
	// build meshClient of hub cluster
	hubMeshClient, err := meshclientset.NewForConfig(hubRestConfig)
	if err != nil {
		return err
	}

	// create an agent contoller
	agent := newAgentController(
		// hubKubeClient,
		hubMeshClient,
		spokeKubeInformerFactory.Core().V2().ServiceMeshControlPlanes(),
		spokeKubeInformerFactory.Core().V1().ServiceMeshMemberRolls(),
		o.SpokeClusterName,
		o.AddonNamespace,
		controllerContext.EventRecorder,
	)
	// create a lease updater
	leaseUpdater := lease.NewLeaseUpdater(
		spokeKubeClient,
		o.AddonName,
		o.AddonNamespace,
	)

	go spokeKubeInformerFactory.Start(ctx.Done())
	go agent.Run(ctx, 1)
	go leaseUpdater.Start(ctx)

	<-ctx.Done()
	return nil
}

type agentController struct {
	// hubKubeClient    kubernetes.Interface
	hubMeshClient   meshclientset.Interface
	spokeSMCPLister maistrav2lister.ServiceMeshControlPlaneLister
	spokeSMMRLister maistrav1lister.ServiceMeshMemberRollLister
	clusterName     string
	addonNamespace  string
	recorder        events.Recorder
}

func newAgentController(
	// hubKubeClient kubernetes.Interface,
	hubMeshClient meshclientset.Interface,
	smcpInformer maistrav2informer.ServiceMeshControlPlaneInformer,
	smmrInformer maistrav1informer.ServiceMeshMemberRollInformer,
	clusterName string,
	addonNamespace string,
	recorder events.Recorder,
) factory.Controller {
	c := &agentController{
		// hubKubeClient:    hubKubeClient,
		hubMeshClient:   hubMeshClient,
		clusterName:     clusterName,
		addonNamespace:  addonNamespace,
		spokeSMCPLister: smcpInformer.Lister(),
		spokeSMMRLister: smmrInformer.Lister(),
		recorder:        recorder,
	}
	return factory.New().WithInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, smcpInformer.Informer()).
		WithSync(c.sync).ToController("multicluster-mesh-agent-controller", recorder)
}

func (c *agentController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling SMCP %q", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	smcp, err := c.spokeSMCPLister.ServiceMeshControlPlanes(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// smmr named "default" in the namespace
	smmr, err := c.spokeSMMRLister.ServiceMeshMemberRolls(namespace).Get("default")
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	mesh, err := meshtranslate.TranslateToLogicMesh(smcp, smmr, c.clusterName)
	if err != nil {
		return err
	}

	_, _, err = meshresourceapply.ApplyMesh(ctx, c.hubMeshClient.MeshV1alpha1(), c.recorder, mesh)
	return err
}
