package agent

import (
	"context"
	"embed"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	"github.com/spf13/cobra"
	istioclientset "istio.io/client-go/pkg/clientset/versioned"
	crdclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
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
	utils "github.com/stolostron/multicluster-mesh-addon/pkg/utils"
)

//go:embed manifests
var fs embed.FS
var (
	istiooperatorCrd = "manifests/crd-istiooperator.yaml"
	ossmSmcpCrd      = "manifests/crd-smcp.yaml"
	ossmSmmrCrd      = "manifests/crd-smmr.yaml"
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

	// build kubeclient of managed cluster
	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// build spoke kube informer factory
	spokeKubeInformerFactory := informers.NewSharedInformerFactory(spokeKubeClient, 10*time.Minute)

	// build the spoke client for CRD
	spokeCrdClient, err := crdclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// check if current managed cluster is an openshift cluster
	isOpenshift := utils.IsOpenshift(spokeKubeClient)
	if isOpenshift { // handle openshift managed clusters
		// apply the CRDs for SMCP and SMMR
		results := resourceapply.ApplyDirectly(ctx,
			resourceapply.NewClientHolder().WithAPIExtensionsClient(spokeCrdClient),
			controllerContext.EventRecorder,
			resourceapply.NewResourceCache(),
			func(name string) ([]byte, error) {
				template, err := fs.ReadFile(name)
				if err != nil {
					return nil, err
				}
				return template, err
			},
			ossmSmcpCrd,
			ossmSmmrCrd,
		)
		for _, result := range results {
			if result.Error != nil {
				return result.Error
			}
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

		// build spoke maistra informer factory
		spokeMaistraInformerFactory := maistrainformer.NewSharedInformerFactory(spokeMaistraClient, 10*time.Minute)

		// create an ossm discovery controller
		ossmDiscoveryController := meshdiscovery.NewOSSMDiscoveryController(
			o.SpokeClusterName,
			o.AddonNamespace,
			hubMeshClient,
			spokeMaistraInformerFactory.Core().V2().ServiceMeshControlPlanes(),
			spokeMaistraInformerFactory.Core().V1().ServiceMeshMemberRolls(),
			controllerContext.EventRecorder,
		)

		// create an ossm mesh-deploy controller
		ossmDeployController := meshdeploy.NewOSSMDeployController(
			o.SpokeClusterName,
			o.AddonNamespace,
			hubMeshClient,
			hubMeshInformerFactory.Mesh().V1alpha1().Meshes(),
			spokeKubeClient,
			spokeOLMClient,
			spokeMaistraClient,
			controllerContext.EventRecorder,
		)

		// create an ossm mesh-federation controller
		ossmFederationController := meshfederation.NewOSSMFederationController(
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

		go spokeMaistraInformerFactory.Start(ctx.Done())
		go ossmDiscoveryController.Run(ctx, 1)
		go ossmDeployController.Run(ctx, 1)
		go ossmFederationController.Run(ctx, 1)
	} else { // handle non-openshift(*KS) managed clusters
		results := resourceapply.ApplyDirectly(ctx,
			resourceapply.NewClientHolder().WithAPIExtensionsClient(spokeCrdClient),
			controllerContext.EventRecorder,
			resourceapply.NewResourceCache(),
			func(name string) ([]byte, error) {
				template, err := fs.ReadFile(name)
				if err != nil {
					return nil, err
				}
				return template, err
			},
			istiooperatorCrd,
		)
		for _, result := range results {
			if result.Error != nil {
				return result.Error
			}
		}

		// build dynamic client of managed cluster
		spokeDynamicClient, err := dynamic.NewForConfig(controllerContext.KubeConfig)
		if err != nil {
			return err
		}

		// build spoke dynamic informer factory
		spokeDynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(spokeDynamicClient, 10*time.Minute)

		// build spoke istio api client
		spokeIstioApiClient, err := istioclientset.NewForConfig(controllerContext.KubeConfig)
		if err != nil {
			return err
		}

		// create an upstream istio discovery controller
		istioDiscoveryController := meshdiscovery.NewIstioDiscoveryController(
			o.SpokeClusterName,
			o.AddonNamespace,
			hubMeshClient,
			spokeKubeClient,
			spokeDynamicInformerFactory.ForResource(schema.GroupVersionResource{Group: "install.istio.io", Version: "v1alpha1", Resource: "istiooperators"}),
			controllerContext.EventRecorder,
		)

		// create an istio mesh-deploy controller
		istioDeployController := meshdeploy.NewIstioDeployController(
			o.SpokeClusterName,
			o.AddonNamespace,
			hubMeshClient,
			hubMeshInformerFactory.Mesh().V1alpha1().Meshes(),
			spokeDynamicClient,
			spokeKubeClient,
			spokeIstioApiClient,
			controllerContext.EventRecorder,
		)

		// create an istio mesh-federation controller
		istioFederationController := meshfederation.NewIstioFederationController(
			o.SpokeClusterName,
			o.AddonNamespace,
			hubKubeClient,
			spokeKubeClient,
			hubKubeInformerFactory.Core().V1().Secrets(),
			hubMeshInformerFactory.Mesh().V1alpha1().Meshes(),
			controllerContext.EventRecorder,
		)

		go spokeDynamicInformerFactory.Start(ctx.Done())
		go istioDiscoveryController.Run(ctx, 1)
		go istioDeployController.Run(ctx, 1)
		go istioFederationController.Run(ctx, 1)
	}

	// create a lease updater
	leaseUpdater := lease.NewLeaseUpdater(
		spokeKubeClient,
		o.AddonName,
		o.AddonNamespace,
	)

	go hubKubeInformerFactory.Start(ctx.Done())
	go spokeKubeInformerFactory.Start(ctx.Done())
	go hubMeshInformerFactory.Start(ctx.Done())
	go leaseUpdater.Start(ctx)

	<-ctx.Done()
	return nil
}
