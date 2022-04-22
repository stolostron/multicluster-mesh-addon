package resourceapply

import (
	"context"
	"reflect"

	istionetworkv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiov1alpha3client "istio.io/client-go/pkg/clientset/versioned/typed/networking/v1alpha3"
	equality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/operator/events"
	openshiftresourceapply "github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
)

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

func ApplyIstioGateway(ctx context.Context, client istiov1alpha3client.GatewaysGetter, recorder events.Recorder, required *istionetworkv1alpha3.Gateway) (*istionetworkv1alpha3.Gateway, bool, error) {
	existing, err := client.Gateways(required.Namespace).Get(context.TODO(), required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.Gateways(requiredCopy.Namespace).
			Create(context.TODO(), resourcemerge.WithCleanLabelsAndAnnotations(requiredCopy).(*istionetworkv1alpha3.Gateway), metav1.CreateOptions{})
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

	klog.V(2).Infof("Istio Gateway %q changes: %v", required.Namespace+"/"+required.Name, openshiftresourceapply.JSONPatchNoError(existing, required))
	actual, err := client.Gateways(required.Namespace).Update(context.TODO(), existingCopy, metav1.UpdateOptions{})
	reportUpdateEvent(recorder, required, err)
	return actual, true, err
}
