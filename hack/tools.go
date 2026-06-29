//go:build tools

// Package tools tracks tool dependencies so they end up in go.sum and
// can be installed via `go install`.
package tools

import (
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
	_ "sigs.k8s.io/kustomize/kustomize/v5"
)
