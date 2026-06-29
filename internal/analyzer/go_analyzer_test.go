package analyzer

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGoAnalyzer_Resources(t *testing.T) {
	tests := []struct {
		name     string
		observed ObservedMetrics
		current  CurrentSettings
		want     []Recommendation
	}{
		{
			name: "requests.cpu lifted when p95 > current",
			observed: ObservedMetrics{
				CPUMillicoresP95: 340,
				CPUMillicoresP99: 510,
			},
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
				},
			},
			want: []Recommendation{
				{
					Field:       "resources.requests.cpu",
					Current:     "100m",
					Recommended: "340m",
					Rationale:   "p95 CPU usage 340m exceeds requests 100m; scheduler will mispack the pod.",
					Severity:    SeverityWarning,
				},
				{
					Field:       "resources.limits.cpu",
					Current:     "500m",
					Recommended: "638m", // 510 * 1.25 = 637.5 → 638
					Rationale:   "p99 CPU usage 510m needs 25% headroom over current limit 500m.",
					Severity:    SeverityInfo,
				},
			},
		},
		{
			name: "requests.memory bumped when p95 working-set > current",
			observed: ObservedMetrics{
				MemoryWorkingSetP95Bytes: 398_458_880, // 380Mi
				MemoryWorkingSetP99Bytes: 440_401_920, // 420Mi
			},
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				},
			},
			want: []Recommendation{
				{
					Field:       "resources.requests.memory",
					Current:     "256Mi",
					Recommended: "380Mi",
					Rationale:   "p95 memory working set 380Mi exceeds requests 256Mi.",
					Severity:    SeverityWarning,
				},
				{
					Field:       "resources.limits.memory",
					Current:     "512Mi",
					Recommended: "504Mi", // 420 * 1.20 = 504
					Rationale:   "p99 memory working set 420Mi needs 20% headroom; current limit 512Mi is close.",
					Severity:    SeverityWarning,
				},
			},
		},
		{
			name: "no change when within 10% suppression threshold",
			observed: ObservedMetrics{
				CPUMillicoresP95: 105, // current is 100m, delta 5%
			},
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				},
			},
			want: nil, // suppressed
		},
		{
			name: "floor 50m for requests.cpu when p95 is tiny",
			observed: ObservedMetrics{
				CPUMillicoresP95: 10,
			},
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
				},
			},
			want: []Recommendation{
				{
					Field:       "resources.requests.cpu",
					Current:     "100m",
					Recommended: "50m",
					Rationale:   "p95 CPU usage 10m is well below requests 100m; floor 50m applied.",
					Severity:    SeverityWarning,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := GoAnalyzer{}
			got := a.Analyze(tc.observed, tc.current)
			// Filter out env recommendations for this resources-only test;
			// env coverage is in Task 8.
			got = onlyFields(got, "resources.")
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("recommendations mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func onlyFields(recs []Recommendation, prefix string) []Recommendation {
	var out []Recommendation
	for _, r := range recs {
		if startsWith(r.Field, prefix) {
			out = append(out, r)
		}
	}
	return out
}

func startsWith(s, p string) bool {
	if len(s) < len(p) {
		return false
	}
	return s[:len(p)] == p
}
