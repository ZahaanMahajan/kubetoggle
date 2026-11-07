package kubernetes

import (
	"fmt"

	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// discardWarningHandler prevents Kubernetes API warnings from being
// written to the terminal while the TUI is running.
type discardWarningHandler struct{}

func (discardWarningHandler) HandleWarningHeader(code int, agent, text string) {}

// NewClientset creates a Kubernetes client with the default warning behavior.
func NewClientset() (*k8sclient.Clientset, error) {
	config, err := newRESTConfig(nil)
	if err != nil {
		return nil, err
	}

	clientset, err := k8sclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return clientset, nil
}

// NewTUIClientset creates a Kubernetes client that suppresses API warnings.
func NewTUIClientset() (*k8sclient.Clientset, error) {
	config, err := newRESTConfig(discardWarningHandler{})
	if err != nil {
		return nil, err
	}

	clientset, err := k8sclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return clientset, nil
}

// NewMetricsClient creates a client for the Kubernetes Metrics API.
func NewMetricsClient() (*metricsclient.Clientset, error) {
	config, err := newRESTConfig(nil)
	if err != nil {
		return nil, err
	}

	clientset, err := metricsclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Metrics API client: %w", err)
	}

	return clientset, nil
}

// NewTUIMetricsClient creates a Metrics API client that suppresses warnings.
func NewTUIMetricsClient() (*metricsclient.Clientset, error) {
	config, err := newRESTConfig(discardWarningHandler{})
	if err != nil {
		return nil, err
	}

	clientset, err := metricsclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Metrics API client: %w", err)
	}

	return clientset, nil
}

// newRESTConfig loads in-cluster credentials or falls back to kubeconfig.
func newRESTConfig(warningHandler rest.WarningHandler) (*rest.Config, error) {
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

	return config, nil
}
