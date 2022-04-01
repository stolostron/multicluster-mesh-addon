package resourceapply

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	equality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtime "k8s.io/apimachinery/pkg/runtime"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
	openshiftresourceapply "github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	olmv1 "github.com/operator-framework/api/pkg/operators/v1"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	olmv1client "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1"
	olmv1alpha1client "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1alpha1"
	maistrav1client "maistra.io/api/client/versioned/typed/core/v1"
	maistrav2client "maistra.io/api/client/versioned/typed/core/v2"
	maistrafederationv1client "maistra.io/api/client/versioned/typed/federation/v1"
	maistrav1 "maistra.io/api/core/v1"
	maistrav2 "maistra.io/api/core/v2"
	maistrafederationv1 "maistra.io/api/federation/v1"

	meshv1alpha1client "github.com/stolostron/multicluster-mesh-addon/apis/client/clientset/versioned/typed/mesh/v1alpha1"
	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/apis/mesh/v1alpha1"
)

func ApplyMesh(ctx context.Context, client meshv1alpha1client.MeshesGetter, recorder events.Recorder, required *meshv1alpha1.Mesh) (*meshv1alpha1.Mesh, bool, error) {
	existing, err := client.Meshes(required.Namespace).Get(context.TODO(), required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.Meshes(requiredCopy.Namespace).
			Create(context.TODO(), resourcemerge.WithCleanLabelsAndAnnotations(requiredCopy).(*meshv1alpha1.Mesh), metav1.CreateOptions{})
		reportCreateEvent(recorder, requiredCopy, err)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	specSame := reflect.DeepEqual(&existingCopy.Spec, required.Spec)
	if !*modified && specSame {
		return existingCopy, false, nil
	}
	if !specSame {
		existingCopy.Spec = required.Spec
	}

	klog.V(2).Infof("Mesh %q changes: %v", required.Namespace+"/"+required.Name, openshiftresourceapply.JSONPatchNoError(existing, required))
	actual, err := client.Meshes(required.Namespace).Update(context.TODO(), existingCopy, metav1.UpdateOptions{})
	reportUpdateEvent(recorder, required, err)
	return actual, true, err
}

func ApplySubscription(ctx context.Context, client olmv1alpha1client.SubscriptionsGetter, recorder events.Recorder, required *olmv1alpha1.Subscription) (*olmv1alpha1.Subscription, bool, error) {
	existing, err := client.Subscriptions(required.Namespace).Get(context.TODO(), required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.Subscriptions(requiredCopy.Namespace).
			Create(context.TODO(), resourcemerge.WithCleanLabelsAndAnnotations(requiredCopy).(*olmv1alpha1.Subscription), metav1.CreateOptions{})
		reportCreateEvent(recorder, requiredCopy, err)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	specSame := existingCopy.Spec.Channel == required.Spec.Channel &&
		existingCopy.Spec.Package == required.Spec.Package &&
		existingCopy.Spec.CatalogSource == required.Spec.CatalogSource &&
		existingCopy.Spec.CatalogSourceNamespace == required.Spec.CatalogSourceNamespace &&
		existingCopy.Spec.InstallPlanApproval == required.Spec.InstallPlanApproval

	if !*modified && specSame {
		return existingCopy, false, nil
	}
	if !specSame {
		existingCopy.Spec = required.Spec
	}

	klog.V(2).Infof("Subscription %q changes: %v", required.Namespace+"/"+required.Name, openshiftresourceapply.JSONPatchNoError(existing, required))
	actual, err := client.Subscriptions(required.Namespace).Update(context.TODO(), existingCopy, metav1.UpdateOptions{})
	reportUpdateEvent(recorder, required, err)
	return actual, true, err
}

func ApplyOperatorGroup(ctx context.Context, client olmv1client.OperatorGroupsGetter, recorder events.Recorder, required *olmv1.OperatorGroup) (*olmv1.OperatorGroup, bool, error) {
	existing, err := client.OperatorGroups(required.Namespace).Get(context.TODO(), required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.OperatorGroups(requiredCopy.Namespace).
			Create(context.TODO(), resourcemerge.WithCleanLabelsAndAnnotations(requiredCopy).(*olmv1.OperatorGroup), metav1.CreateOptions{})
		reportCreateEvent(recorder, requiredCopy, err)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	specSame := reflect.DeepEqual(&existingCopy.Spec, required.Spec)

	if !*modified && specSame {
		return existingCopy, false, nil
	}
	if !specSame {
		existingCopy.Spec = required.Spec
	}

	klog.V(2).Infof("OperatorGroup %q changes: %v", required.Namespace+"/"+required.Name, openshiftresourceapply.JSONPatchNoError(existing, required))
	actual, err := client.OperatorGroups(required.Namespace).Update(context.TODO(), existingCopy, metav1.UpdateOptions{})
	reportUpdateEvent(recorder, required, err)
	return actual, true, err
}

type mimicDefaultingFunc func(obj *unstructured.Unstructured)

func noDefaulting(obj *unstructured.Unstructured) {}

type equalityChecker interface {
	DeepEqual(a1, a2 interface{}) bool
}

func ApplyIstioOperator(ctx context.Context, client dynamic.Interface, recorder events.Recorder, required *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	iopGVR := schema.GroupVersionResource{Group: "install.istio.io", Version: "v1alpha1", Resource: "istiooperators"}
	namespace := required.GetNamespace()
	existing, err := client.Resource(iopGVR).Namespace(namespace).Get(ctx, required.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		newObj, createErr := client.Resource(iopGVR).Namespace(namespace).Create(ctx, required, metav1.CreateOptions{})
		if createErr != nil {
			recorder.Warningf("IstioOperator Create Failed", "Failed to create istiooperators.install.istio.io/v1alpha1: %v", createErr)
			return nil, true, createErr
		}
		recorder.Eventf("IstioOperator Created", "Created istiooperators.install.istio.io/v1alpha1 because it was missing")
		return newObj, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()
	// TODO(morvencao): merged the IOP with profile before checking
	toUpdate, modified, err := ensureGenericSpec(required, existingCopy, noDefaulting, equality.Semantic)
	if err != nil {
		return nil, false, err
	}

	if !modified {
		return nil, false, nil
	}

	if klog.V(2).Enabled() {
		klog.Infof("IstioOperator %q changes: %v", namespace+"/"+required.GetName(), openshiftresourceapply.JSONPatchNoError(existing, toUpdate))
	}

	newObj, err := client.Resource(iopGVR).Namespace(namespace).Update(ctx, toUpdate, metav1.UpdateOptions{})
	if err != nil {
		recorder.Warningf("IstioOperator Update Failed", "Failed to update istiooperators.install.istio.io/v1alpha1: %v", err)
		return nil, true, err
	}

	recorder.Eventf("IstioOperator Updated", "Updated istiooperators.install.istio.io/v1alpha1 because it changed")
	return newObj, true, err
}

func ensureGenericSpec(required, existing *unstructured.Unstructured, mimicDefaultingFn mimicDefaultingFunc, equalityChecker equalityChecker) (*unstructured.Unstructured, bool, error) {
	requiredCopy := required.DeepCopy()
	mimicDefaultingFn(requiredCopy)
	requiredSpec, _, err := unstructured.NestedMap(requiredCopy.UnstructuredContent(), "spec")
	if err != nil {
		return nil, false, err
	}
	existingSpec, _, err := unstructured.NestedMap(existing.UnstructuredContent(), "spec")
	if err != nil {
		return nil, false, err
	}

	if equalityChecker.DeepEqual(existingSpec, requiredSpec) {
		return existing, false, nil
	}

	existingCopy := existing.DeepCopy()
	if err := unstructured.SetNestedMap(existingCopy.UnstructuredContent(), requiredSpec, "spec"); err != nil {
		return nil, true, err
	}

	return existingCopy, true, nil
}

func ApplyServiceMeshControlPlane(ctx context.Context, client maistrav2client.ServiceMeshControlPlanesGetter, recorder events.Recorder, required *maistrav2.ServiceMeshControlPlane) (*maistrav2.ServiceMeshControlPlane, bool, error) {
	existing, err := client.ServiceMeshControlPlanes(required.Namespace).Get(context.TODO(), required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.ServiceMeshControlPlanes(requiredCopy.Namespace).
			Create(context.TODO(), resourcemerge.WithCleanLabelsAndAnnotations(requiredCopy).(*maistrav2.ServiceMeshControlPlane), metav1.CreateOptions{})
		reportCreateEvent(recorder, requiredCopy, err)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	specSame := reflect.DeepEqual(&existingCopy.Spec, required.Spec)
	if !*modified && specSame {
		return existingCopy, false, nil
	}
	if !specSame {
		existingCopy.Spec = required.Spec
	}

	klog.V(2).Infof("ServiceMeshControlPlane %q changes: %v", required.Namespace+"/"+required.Name, openshiftresourceapply.JSONPatchNoError(existing, required))
	actual, err := client.ServiceMeshControlPlanes(required.Namespace).Update(context.TODO(), existingCopy, metav1.UpdateOptions{})
	reportUpdateEvent(recorder, required, err)
	return actual, true, err
}

func ApplyServiceMeshMemberRoll(ctx context.Context, client maistrav1client.ServiceMeshMemberRollsGetter, recorder events.Recorder, required *maistrav1.ServiceMeshMemberRoll) (*maistrav1.ServiceMeshMemberRoll, bool, error) {
	existing, err := client.ServiceMeshMemberRolls(required.Namespace).Get(context.TODO(), required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.ServiceMeshMemberRolls(requiredCopy.Namespace).
			Create(context.TODO(), resourcemerge.WithCleanLabelsAndAnnotations(requiredCopy).(*maistrav1.ServiceMeshMemberRoll), metav1.CreateOptions{})
		reportCreateEvent(recorder, requiredCopy, err)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	specSame := reflect.DeepEqual(&existingCopy.Spec, required.Spec)
	if !*modified && specSame {
		return existingCopy, false, nil
	}
	if !specSame {
		existingCopy.Spec = required.Spec
	}

	klog.V(2).Infof("ServiceMeshMemberRoll %q changes: %v", required.Namespace+"/"+required.Name, openshiftresourceapply.JSONPatchNoError(existing, required))
	actual, err := client.ServiceMeshMemberRolls(required.Namespace).Update(context.TODO(), existingCopy, metav1.UpdateOptions{})
	reportUpdateEvent(recorder, required, err)
	return actual, true, err
}

func ApplyServiceMeshPeer(ctx context.Context, client maistrafederationv1client.ServiceMeshPeersGetter, recorder events.Recorder, required *maistrafederationv1.ServiceMeshPeer) (*maistrafederationv1.ServiceMeshPeer, bool, error) {
	existing, err := client.ServiceMeshPeers(required.Namespace).Get(context.TODO(), required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.ServiceMeshPeers(requiredCopy.Namespace).
			Create(context.TODO(), resourcemerge.WithCleanLabelsAndAnnotations(requiredCopy).(*maistrafederationv1.ServiceMeshPeer), metav1.CreateOptions{})
		reportCreateEvent(recorder, requiredCopy, err)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	specSame := reflect.DeepEqual(&existingCopy.Spec, required.Spec)
	if !*modified && specSame {
		return existingCopy, false, nil
	}
	if !specSame {
		existingCopy.Spec = required.Spec
	}

	klog.V(2).Infof("ServiceMeshPeer %q changes: %v", required.Namespace+"/"+required.Name, openshiftresourceapply.JSONPatchNoError(existing, required))
	actual, err := client.ServiceMeshPeers(required.Namespace).Update(context.TODO(), existingCopy, metav1.UpdateOptions{})
	reportUpdateEvent(recorder, required, err)
	return actual, true, err
}

func reportCreateEvent(recorder events.Recorder, obj runtime.Object, originalErr error) {
	gvk := resourcehelper.GuessObjectGroupVersionKind(obj)
	if originalErr == nil {
		recorder.Eventf(fmt.Sprintf("%sCreated", gvk.Kind), "Created %s because it was missing", resourcehelper.FormatResourceForCLIWithNamespace(obj))
		return
	}
	recorder.Warningf(fmt.Sprintf("%sCreateFailed", gvk.Kind), "Failed to create %s: %v", resourcehelper.FormatResourceForCLIWithNamespace(obj), originalErr)
}

func reportUpdateEvent(recorder events.Recorder, obj runtime.Object, originalErr error, details ...string) {
	gvk := resourcehelper.GuessObjectGroupVersionKind(obj)
	switch {
	case originalErr != nil:
		recorder.Warningf(fmt.Sprintf("%sUpdateFailed", gvk.Kind), "Failed to update %s: %v", resourcehelper.FormatResourceForCLIWithNamespace(obj), originalErr)
	case len(details) == 0:
		recorder.Eventf(fmt.Sprintf("%sUpdated", gvk.Kind), "Updated %s because it changed", resourcehelper.FormatResourceForCLIWithNamespace(obj))
	default:
		recorder.Eventf(fmt.Sprintf("%sUpdated", gvk.Kind), "Updated %s:\n%s", resourcehelper.FormatResourceForCLIWithNamespace(obj), strings.Join(details, "\n"))
	}
}
