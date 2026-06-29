// Package k8s builds a Kubernetes clientset for the harness.
//
// It prefers in-cluster config (when the harness runs as a pod) and falls back
// to a local kubeconfig (KUBECONFIG, an explicit path, or ~/.kube/config) for
// local runs against kind.
package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClientset builds a clientset. kubeconfigPath may be empty to use the
// default resolution order.
func NewClientset(kubeconfigPath string) (kubernetes.Interface, error) {
	cfg, err := loadConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build clientset: %w", err)
	}
	return cs, nil
}

func loadConfig(kubeconfigPath string) (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}

	path := kubeconfigPath
	if path == "" {
		path = os.Getenv("KUBECONFIG")
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir for kubeconfig: %w", err)
		}
		path = filepath.Join(home, ".kube", "config")
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %s: %w", path, err)
	}
	return cfg, nil
}
