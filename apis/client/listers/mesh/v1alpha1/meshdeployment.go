/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/stolostron/multicluster-mesh-addon/apis/mesh/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// MeshDeploymentLister helps list MeshDeployments.
// All objects returned here must be treated as read-only.
type MeshDeploymentLister interface {
	// List lists all MeshDeployments in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.MeshDeployment, err error)
	// MeshDeployments returns an object that can list and get MeshDeployments.
	MeshDeployments(namespace string) MeshDeploymentNamespaceLister
	MeshDeploymentListerExpansion
}

// meshDeploymentLister implements the MeshDeploymentLister interface.
type meshDeploymentLister struct {
	indexer cache.Indexer
}

// NewMeshDeploymentLister returns a new MeshDeploymentLister.
func NewMeshDeploymentLister(indexer cache.Indexer) MeshDeploymentLister {
	return &meshDeploymentLister{indexer: indexer}
}

// List lists all MeshDeployments in the indexer.
func (s *meshDeploymentLister) List(selector labels.Selector) (ret []*v1alpha1.MeshDeployment, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.MeshDeployment))
	})
	return ret, err
}

// MeshDeployments returns an object that can list and get MeshDeployments.
func (s *meshDeploymentLister) MeshDeployments(namespace string) MeshDeploymentNamespaceLister {
	return meshDeploymentNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// MeshDeploymentNamespaceLister helps list and get MeshDeployments.
// All objects returned here must be treated as read-only.
type MeshDeploymentNamespaceLister interface {
	// List lists all MeshDeployments in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.MeshDeployment, err error)
	// Get retrieves the MeshDeployment from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.MeshDeployment, error)
	MeshDeploymentNamespaceListerExpansion
}

// meshDeploymentNamespaceLister implements the MeshDeploymentNamespaceLister
// interface.
type meshDeploymentNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all MeshDeployments in the indexer for a given namespace.
func (s meshDeploymentNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.MeshDeployment, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.MeshDeployment))
	})
	return ret, err
}

// Get retrieves the MeshDeployment from the indexer for a given namespace and name.
func (s meshDeploymentNamespaceLister) Get(name string) (*v1alpha1.MeshDeployment, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("meshdeployment"), name)
	}
	return obj.(*v1alpha1.MeshDeployment), nil
}
