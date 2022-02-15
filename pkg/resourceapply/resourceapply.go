package resourceapply

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
	openshiftresourceapply "github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
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