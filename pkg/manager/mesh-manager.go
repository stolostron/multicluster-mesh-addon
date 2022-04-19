package manager

import (
	"context"
	"embed"
	"fmt"
	"os"
	"time"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"open-cluster-management.io/addon-framework/pkg/version"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	meshclientset "github.com/stolostron/multicluster-mesh-addon/apis/client/clientset/versioned"
	meshinformer "github.com/stolostron/multicluster-mesh-addon/apis/client/informers/externalversions"
	constants "github.com/stolostron/multicluster-mesh-addon/pkg/constants"
	meshdeployment "github.com/stolostron/multicluster-mesh-addon/pkg/manager/deployment"
	meshfederation "github.com/stolostron/multicluster-mesh-addon/pkg/manager/federation"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

//go:embed manifests
//go:embed manifests/agent
var fs embed.FS

var agentRBACFiles = []string{
	// role with RBAC rules to access resources on hub
	"manifests/rbac/role.yaml",
	// rolebinding to bind the above role to a certain user group
	"manifests/rbac/rolebinding.yaml",
}

func NewControllerCommand() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("multicluster-mesh-addon-controller", version.Get(), runController).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the multicluster mesh addon controller"

	return cmd
}

func runController(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	registrationOption := newRegistrationOption(
		controllerContext.KubeConfig,
		controllerContext.EventRecorder,
		utilrand.String(5))

	agentAddon, err := addonfactory.NewAgentAddonFactory(constants.MeshAddonName, fs, "manifests/agent").
		WithGetValuesFuncs(getValues, addonfactory.GetValuesFromAddonAnnotation).
		WithAgentRegistrationOption(registrationOption).
		WithInstallStrategy(agent.InstallAllStrategy(constants.MeshAgentNamespace)).
		BuildTemplateAgentAddon()
	if err != nil {
		klog.Errorf("failed to build agent %v", err)
		return err
	}

	err = mgr.AddAgent(agentAddon)
	if err != nil {
		klog.Fatal(err)
	}

	// build kube client
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)

	// build kube informer factory
	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, 10*time.Minute)

	// build mesh kubeconfig
	meshClient, err := meshclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	// build mesh informer factory
	meshInformerFactory := meshinformer.NewSharedInformerFactory(meshClient, 10*time.Minute)

	// create an meshDeployment controller
	meshDeploymentController := meshdeployment.NewMeshDeploymentController(
		meshClient,
		meshInformerFactory.Mesh().V1alpha1().MeshDeployments(),
		controllerContext.EventRecorder,
	)

	// create an meshFederation controller
	meshFederationController := meshfederation.NewMeshFederationController(
		kubeClient,
		meshClient,
		kubeInformerFactory.Core().V1().ConfigMaps(),
		kubeInformerFactory.Core().V1().Secrets(),
		meshInformerFactory.Mesh().V1alpha1().MeshFederations(),
		controllerContext.EventRecorder,
	)

	err = mgr.Start(ctx)
	if err != nil {
		klog.Fatal(err)
	}

	go kubeInformerFactory.Start(ctx.Done())
	go meshInformerFactory.Start(ctx.Done())
	go meshDeploymentController.Run(ctx, 1)
	go meshFederationController.Run(ctx, 1)
	<-ctx.Done()

	return nil
}

func newRegistrationOption(kubeConfig *rest.Config, recorder events.Recorder, agentName string) *agent.RegistrationOption {
	return &agent.RegistrationOption{
		CSRConfigurations: agent.KubeClientSignerConfigurations(constants.MeshAddonName, agentName),
		CSRApproveCheck:   utils.DefaultCSRApprover(agentName),
		PermissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
			kubeclient, err := kubernetes.NewForConfig(kubeConfig)
			if err != nil {
				return err
			}

			for _, file := range agentRBACFiles {
				if err := applyManifestFromFile(file, cluster.Name, addon.Name, kubeclient, recorder); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func applyManifestFromFile(file, clusterName, addonName string, kubeclient *kubernetes.Clientset, recorder events.Recorder) error {
	groups := agent.DefaultGroups(clusterName, addonName)
	config := struct {
		ClusterName string
		Group       string
	}{
		ClusterName: clusterName,
		Group:       groups[0],
	}

	results := resourceapply.ApplyDirectly(context.Background(),
		resourceapply.NewKubeClientHolder(kubeclient),
		recorder,
		resourceapply.NewResourceCache(),
		func(name string) ([]byte, error) {
			template, err := fs.ReadFile(file)
			if err != nil {
				return nil, err
			}
			return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
		},
		file,
	)

	for _, result := range results {
		if result.Error != nil {
			return result.Error
		}
	}

	return nil
}

func getValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = constants.MeshAgentNamespace
	}

	image := os.Getenv("MULTICLUSTER_MESH_ADDON_IMAGE")
	if len(image) == 0 {
		image = constants.DefaultMeshAddonImage
	}

	manifestConfig := struct {
		KubeConfigSecret      string
		ClusterName           string
		AddonInstallNamespace string
		Image                 string
	}{
		KubeConfigSecret:      fmt.Sprintf("%s-hub-kubeconfig", addon.Name),
		AddonInstallNamespace: installNamespace,
		ClusterName:           cluster.Name,
		Image:                 image,
	}

	return addonfactory.StructToValues(manifestConfig), nil
}
