package server

import (
	"github.com/builderhub/build-api/internal/k8s"
)

// K8sClient wraps the k8s client for use by BuildAPIService.
type K8sClient = k8s.Client

// NewK8sClient creates a K8s client.
func NewK8sClient(kubeconfig string) (*K8sClient, error) {
	return k8s.NewClient(kubeconfig)
}
