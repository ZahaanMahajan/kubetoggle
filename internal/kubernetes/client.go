package kubernetes

import (
	"fmt"

	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClientset creates a Kubernetes client.
//
// It first attempts to use in-cluster credentials.
// If unavailable, it falls back to the user's kubeconfig.
func NewClientset() (*k8sclient.Clientset, error) {
	config, err := rest.InClusterConfig()

	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

		kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules,
			&clientcmd.ConfigOverrides{},
		)

		config, err = kubeconfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf(
				"failed to load Kubernetes configuration: %w",
				err,
			)
		}
	}

	clientset, err := k8sclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return clientset, nil
}
