package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadRecommendationSpec defines the desired state — auto-populated
// by DiscoveryController from the source Deployment. Users have no
// reason to edit Spec; it is effectively read-only after creation.
type WorkloadRecommendationSpec struct {
	// TargetRef references the Deployment this recommendation is for.
	// Set by DiscoveryController, immutable after creation.
	TargetRef corev1.ObjectReference `json:"targetRef"`
}

// WorkloadRecommendationStatus holds the analyzer output.
type WorkloadRecommendationStatus struct {
	// ObservedGeneration reflects the spec.targetRef generation last analyzed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions reflect the current state of analysis.
	// Standard k8s pattern; well-known types: Ready, MetricsAvailable,
	// WorkloadDetected, DataSufficient.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// LastAnalyzedAt is the time of the most recent successful analysis.
	// +optional
	LastAnalyzedAt *metav1.Time `json:"lastAnalyzedAt,omitempty"`

	// Workload describes what was detected.
	// +optional
	Workload *WorkloadInfo `json:"workload,omitempty"`

	// Observed metrics from Prometheus.
	// +optional
	Observed *ObservedMetrics `json:"observed,omitempty"`

	// Current settings read from the target Deployment.
	// +optional
	Current *CurrentSettings `json:"current,omitempty"`

	// Recommendations produced by the analyzer.
	// +optional
	Recommendations []Recommendation `json:"recommendations,omitempty"`
}

// WorkloadInfo identifies the runtime detected on the target.
type WorkloadInfo struct {
	// Type is the workload family. Iter 1: "Go".
	Type string `json:"type"`
	// Version is the runtime version, if known (e.g., "1.26.0").
	// +optional
	Version string `json:"version,omitempty"`
}

// ObservedMetrics captures the analysis-window summary.
type ObservedMetrics struct {
	// Window is the duration covered by the metrics (e.g., "24h").
	Window string `json:"window"`

	// CPU usage percentiles in millicores.
	// +optional
	CPU *CPUMetrics `json:"cpu,omitempty"`

	// Memory working-set percentiles in bytes.
	// +optional
	Memory *MemoryMetrics `json:"memory,omitempty"`

	// GC pause percentiles in milliseconds.
	// +optional
	GC *GCMetrics `json:"gc,omitempty"`
}

// CPUMetrics holds CPU usage percentile statistics in millicores.
type CPUMetrics struct {
	P50Millicores int64 `json:"p50Millicores"`
	P95Millicores int64 `json:"p95Millicores"`
	P99Millicores int64 `json:"p99Millicores"`
}

// MemoryMetrics holds container memory working-set percentiles in bytes.
type MemoryMetrics struct {
	WorkingSetP95Bytes int64 `json:"workingSetP95Bytes"`
	WorkingSetP99Bytes int64 `json:"workingSetP99Bytes"`
}

// GCMetrics holds GC pause percentiles.
type GCMetrics struct {
	PauseP99Ms     float64 `json:"pauseP99Ms"`
	PauseP99Window string  `json:"pauseP99Window"`
}

// CurrentSettings captures what the target Deployment is currently configured with.
type CurrentSettings struct {
	// Resources from the target container's spec.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Env values for relevant runtime variables (GOMAXPROCS, GOMEMLIMIT, GOGC).
	// Map keys are env var names; values are the literal string set on the
	// container (empty string means unset).
	// +optional
	Env map[string]string `json:"env,omitempty"`
}

// Recommendation is one suggested change to the target Deployment.
type Recommendation struct {
	// Field is the path the recommendation applies to,
	// e.g. "resources.requests.cpu" or "env.GOMAXPROCS".
	Field string `json:"field"`
	// Current is the present value (or "(unset)").
	Current string `json:"current"`
	// Recommended is the suggested new value.
	Recommended string `json:"recommended"`
	// Rationale is a human-readable explanation.
	Rationale string `json:"rationale"`
	// Severity is one of: info, warning, critical.
	// +kubebuilder:validation:Enum=info;warning;critical
	Severity string `json:"severity"`
}

// WorkloadRecommendation is the Schema for the workloadrecommendations API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wr;workreco
// +kubebuilder:printcolumn:name="Workload",type=string,JSONPath=`.status.workload.type`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Recs",type=integer,JSONPath=`.status.recommendations[*]`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type WorkloadRecommendation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkloadRecommendationSpec   `json:"spec,omitempty"`
	Status WorkloadRecommendationStatus `json:"status,omitempty"`
}

// WorkloadRecommendationList contains a list of WorkloadRecommendation.
// +kubebuilder:object:root=true
type WorkloadRecommendationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkloadRecommendation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkloadRecommendation{}, &WorkloadRecommendationList{})
}
