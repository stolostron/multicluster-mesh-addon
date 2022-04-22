package resourceapply

import (
	"context"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
	openshiftresourceapply "github.com/openshift/library-go/pkg/operator/resource/resourceapply"
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
)

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
