package utils

import "k8s.io/client-go/kubernetes"

func SliceContainsString(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

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
