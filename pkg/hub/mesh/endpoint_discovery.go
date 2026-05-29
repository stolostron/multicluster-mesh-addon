package mesh

import (
	"context"
	"fmt"
	"time"

	meshv1alpha1 "github.com/stolostron/multicluster-mesh-addon/pkg/apis/mesh/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	msav1beta1 "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deployManagedServiceAccountAddon deploys the ManagedServiceAccount add-on for each cluster in the ClusterSet
func (r *Reconciler) deployManagedServiceAccountAddon(ctx context.Context, clusters []clusterv1.ManagedCluster) error {
	if len(clusters) == 0 {
		klog.Info("The ClusterSet has no managed cluster")
		return nil
	}

	klog.Info("Creating ManagedClusterAddOn resources for each managed cluster in the ClusterSet")
	for _, cluster := range clusters {
		existing := &addonv1beta1.ManagedClusterAddOn{}
		if err := r.Get(ctx, types.NamespacedName{Name: "managed-serviceaccount", Namespace: cluster.Name}, existing); err == nil {
			klog.Infof("Cluster %s has managed-serviceaccount addon installed", cluster.Name)
			continue
		} else {
			addon := &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "managed-serviceaccount",
					Namespace: cluster.Name,
					Labels:    map[string]string{ManagedByLabel: ManagedByValue},
				},
			}

			if err := r.Create(ctx, addon); err != nil {
				return fmt.Errorf("failed to deploy managed-serviceaccount addon in cluster %s: %w", cluster.Name, err)
			} else {
				klog.Infof("Successfully deploy managed-serviceaccount addon in cluster %s", cluster.Name)
			}
		}
	}

	return nil
}

// createManagedServiceAccounts creates ManagedServiceAccount resources for each cluster in the ClusterSet
func (r *Reconciler) createManagedServiceAccounts(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	if len(clusters) == 0 {
		klog.Info("The ClusterSet has no managed cluster")
		return nil
	}

	klog.Info("Creating ManagedServiceAccount resources for each managed cluster in the ClusterSet")
	for _, cluster := range clusters {
		existing := &msav1beta1.ManagedServiceAccount{}
		// Naming convention: <cluster-name>-<mesh-name>-istio-reader
		msaName := fmt.Sprintf("%s-%s-istio-reader", cluster.Name, mesh.Name)
		if err := r.Get(ctx, types.NamespacedName{Name: msaName, Namespace: cluster.Name}, existing); err == nil {
			klog.Infof("Cluster %s has an existing ManagedServiceAccount resource %s, skipping createManagedServiceAccount", cluster.Name, msaName)
			continue
		} else {
			duration := mesh.Spec.Security.Discovery.TokenValidity
			// A duration less than 10 minutes causes an error when applying a ManagedServiceAccount CR
			if duration.Minutes() < 10.0 {
				klog.Info("Setting ManagedServiceAccount Validity with minimum value 10 minutes")
				duration = metav1.Duration{Duration: 10 * time.Minute}
			}

			msa := &msav1beta1.ManagedServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msaName,
					Namespace: cluster.Name,
					Labels:    map[string]string{ManagedByLabel: ManagedByValue},
				},
				Spec: msav1beta1.ManagedServiceAccountSpec{
					Rotation: msav1beta1.ManagedServiceAccountRotation{
						Enabled: true, // the ServiceAccount token will be rotated before it expires
						// Validity is the duration of validity for requesting the signed ServiceAccount token
						// Supports hours (h), minutes (m), or seconds (s)
						// User may not specify a duration less than 10 minutes
						Validity: duration,
					},
				},
			}

			if err := r.Create(ctx, msa); err != nil {
				return fmt.Errorf("failed to create a ManagedServiceAccount %s/%s: %w", cluster.Name, msaName, err)
			} else {
				klog.Infof("Successfully created a ManagedServiceAccount %s/%s", cluster.Name, msaName)
			}
		}
	}

	return nil
}

// deleteManagedServiceAccounts deletes ManagedServiceAccount resources managed by multicluster-mesh-addon
func (r *Reconciler) deleteManagedServiceAccounts(ctx context.Context) error {
	msaList := &msav1beta1.ManagedServiceAccountList{}
	if err := r.List(ctx, msaList, client.MatchingLabels{ManagedByLabel: ManagedByValue}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to list ManagedServiceAccount resources managed by multicluster-mesh-addon: %w", err)
	}

	for _, msa := range msaList.Items {
		if err := r.Delete(ctx, &msa); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete a ManagedServiceAccount resource %s: %w", msa.Name, err)
		} else {
			klog.Infof("Deleting a ManagedServiceAccount resource %s", msa.Name)
		}
	}

	return nil
}

// TODO: validate a secondary resource corev1.Secret has the expected ownerReferences
// TODO: build a remote secret "istio-remote-secret-<cluster-name>" using the above secondary resource Secret token and label "istio/multiCluster: "true""

// ensureMsaSecretsDistributed creates ManifestWorks to distribute ManagedServiceAccount secrets to clusters
func (r *Reconciler) ensureMsaSecretsDistributed(ctx context.Context, mesh *meshv1alpha1.MultiClusterMesh, clusters []clusterv1.ManagedCluster) error {
	for _, cluster := range clusters {
		// TODO: replace this "%s-%s-istio-reader" Secret with a remote secret "istio-remote-secret-<cluster-name>"
		secretName := fmt.Sprintf("%s-%s-istio-reader", cluster.Name, mesh.Name)
		if err := r.ensureSecretManifestWork(ctx, secretName, mesh, &cluster); err != nil {
			return fmt.Errorf("failed to ensure ManagedServiceAccount secrets ManifestWork for cluster %s: %w", cluster.Name, err)
		}
	}
	return nil
}
