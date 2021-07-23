package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func objectChanged(previous, current interface{}) bool {
	prev := previous.(metav1.Object)
	cur := current.(metav1.Object)
	return prev.GetResourceVersion() != cur.GetResourceVersion()
}

func networkAnnotationsChanged(previous, current interface{}) bool {
	oldAnnotations := getNetworkAnnotations(previous)
	updatedAnnotations := getNetworkAnnotations(current)
	return oldAnnotations != updatedAnnotations
}

func networkStatusChanged(previous, current interface{}) bool {
	return true
}

func getNetworkAnnotations(obj interface{}) string {
	metaObject := obj.(metav1.Object)
	annotations, ok := metaObject.GetAnnotations()[cncfNetworksKey]
	if !ok {
		return ""
	}
	return annotations
}
