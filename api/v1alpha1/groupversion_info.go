// Package v1alpha1 contains API Schema definitions for the tuna v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=tuna.siabroo.github.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupVersion is group version used to register these objects.
var GroupVersion = schema.GroupVersion{Group: "tuna.siabroo.github.io", Version: "v1alpha1"}

// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion} //nolint:staticcheck // SA1019: scheme.Builder works fine for v1alpha1; api package will be simplified in v1beta1 migration

// AddToScheme adds the types in this group-version to the given scheme.
var AddToScheme = SchemeBuilder.AddToScheme
