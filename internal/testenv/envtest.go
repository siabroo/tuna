// Package testenv provides helpers to spin up a controller-runtime
// envtest control plane and a mock Prometheus HTTP server for
// integration tests. Test-only.
package testenv

import (
	"path/filepath"
	"testing"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	tunav1alpha1 "github.com/siabroo/tuna/api/v1alpha1"
)

// Env exposes the control plane primitives a test needs.
type Env struct {
	Config *rest.Config
	Client client.Client
}

// Start spins up an envtest control plane (etcd + apiserver) and
// registers t.Cleanup to tear it down. Fails the test if start fails.
func Start(t *testing.T) *Env {
	t.Helper()

	env := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	if err := tunav1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	cl, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	return &Env{Config: cfg, Client: cl}
}
