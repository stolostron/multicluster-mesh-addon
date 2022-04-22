package utils

import "k8s.io/client-go/kubernetes"

// SliceContainsString check if the string slice contains the given string
func SliceContainsString(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

// IsOpenshift check if the cluster is openshift cluster with given kubeclient
func IsOpenshift(kubeclient kubernetes.Interface) bool {
	serverGroups, err := kubeclient.Discovery().ServerGroups()
	if err != nil {
		return false
	}
	for _, apiGroup := range serverGroups.Groups {
		if apiGroup.Name == "project.openshift.io" {
			return true
		}
	}
	return false
}
