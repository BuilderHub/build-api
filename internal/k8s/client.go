package k8s

import (
	"path/filepath"

	buildkitv1alpha1 "github.com/builderhub/build-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

// Client wraps controller-runtime client for BuildkitBuilder CRs.
type Client struct {
	ctrl.Client
}

// NewClient creates a K8s client that can manage BuildkitBuilder CRs.
func NewClient(kubeconfig string) (*Client, error) {
	config, err := restConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := buildkitv1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	client, err := ctrl.New(config, ctrl.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return &Client{Client: client}, nil
}

func restConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	// In-cluster config when running inside the cluster
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
