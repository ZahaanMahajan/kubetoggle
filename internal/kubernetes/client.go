package kubernetes

import (
	"fmt"

	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// discardWarningHandler prevents Kubernetes API warnings from being
// written to the terminal while the TUI is running.
type discardWarningHandler struct{}

func (discardWarningHandler) HandleWarningHeader(code int, agent, text string) {}

// NewClientset creates a Kubernetes client with the default warning behavior.
//
// It first attempts to use in-cluster credentials.
// If unavailable, it falls back to the user's kubeconfig.
func NewClientset() (*k8sclient.Clientset, error) {
	return newClientset(nil)
}

// NewTUIClientset creates a Kubernetes client that suppresses API warning
// output to prevent interference with the Bubble Tea terminal interface.
func NewTUIClientset() (*k8sclient.Clientset, error) {
	return newClientset(discardWarningHandler{})
}

func newClientset(warningHandler rest.WarningHandler) (*k8sclient.Clientset, error) {
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

	if warningHandler != nil {
		config.WarningHandler = warningHandler
	}

	clientset, err := k8sclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return clientset, nil
}
