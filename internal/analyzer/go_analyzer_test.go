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
				MemoryWorkingSetP99Bytes: 503_316_480, // 480Mi  ← 12.5% delta vs 512Mi, NOT suppressed
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
					Recommended: "576Mi", // 480 * 1.20 = 576
					Rationale:   "p99 memory working set 480Mi needs 20% headroom; current limit 512Mi is below it.",
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

func TestGoAnalyzer_GOMAXPROCS(t *testing.T) {
	tests := []struct {
		name        string
		current     CurrentSettings
		wantRec     bool
		wantVal     string
		wantSev     string
		wantCurrent string
	}{
		{
			name: "Go < 1.25 + cpu limit + GOMAXPROCS unset → warn",
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("500m"),
				}},
				Env:       map[string]string{},
				GoVersion: "1.23.4",
			},
			wantRec: true,
			wantVal: "1",
			wantSev: SeverityWarning,
		},
		{
			name: "Go >= 1.25 + cpu limit + GOMAXPROCS unset → no rec (Go reads cgroup)",
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("500m"),
				}},
				Env:       map[string]string{},
				GoVersion: "1.26.0",
			},
			wantRec: false,
		},
		{
			name: "Go >= 1.25 + GODEBUG=containermaxprocs=0 (opted out) → warn",
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("500m"),
				}},
				Env: map[string]string{
					"GODEBUG": "containermaxprocs=0",
				},
				GoVersion: "1.26.0",
			},
			wantRec: true,
			wantVal: "1",
			wantSev: SeverityWarning,
		},
		{
			name: "Go >= 1.25 + GOMAXPROCS set wrong → info",
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("500m"),
				}},
				Env: map[string]string{
					"GOMAXPROCS": "4",
				},
				GoVersion: "1.26.0",
			},
			wantRec:     true,
			wantVal:     "1",
			wantSev:     SeverityInfo,
			wantCurrent: "4",
		},
		{
			name: "Go >= 1.25 + GODEBUG opt-out + GOMAXPROCS already set → Current reflects the set value",
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("500m"),
				}},
				Env: map[string]string{
					"GODEBUG":    "containermaxprocs=0",
					"GOMAXPROCS": "4",
				},
				GoVersion: "1.26.0",
			},
			wantRec:     true,
			wantVal:     "1",
			wantSev:     SeverityWarning,
			wantCurrent: "4",
		},
		{
			name: "no cpu limit → no rec (nothing to base it on)",
			current: CurrentSettings{
				Resources: corev1.ResourceRequirements{},
				Env:       map[string]string{},
				GoVersion: "1.26.0",
			},
			wantRec: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GoAnalyzer{}.Analyze(ObservedMetrics{}, tc.current)
			rec := findRec(got, "env.GOMAXPROCS")
			if (rec != nil) != tc.wantRec {
				t.Fatalf("rec present = %v, want %v (got: %+v)", rec != nil, tc.wantRec, got)
			}
			if !tc.wantRec {
				return
			}
			if rec.Recommended != tc.wantVal {
				t.Errorf("Recommended = %q, want %q", rec.Recommended, tc.wantVal)
			}
			if rec.Severity != tc.wantSev {
				t.Errorf("Severity = %q, want %q", rec.Severity, tc.wantSev)
			}
			if tc.wantCurrent != "" && rec.Current != tc.wantCurrent {
				t.Errorf("Current = %q, want %q", rec.Current, tc.wantCurrent)
			}
		})
	}
}

func TestGoAnalyzer_GOMEMLIMIT(t *testing.T) {
	cur := CurrentSettings{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		Env:       map[string]string{},
		GoVersion: "1.26.0",
	}
	got := GoAnalyzer{}.Analyze(ObservedMetrics{}, cur)
	rec := findRec(got, "env.GOMEMLIMIT")
	if rec == nil {
		t.Fatalf("no env.GOMEMLIMIT rec; got: %+v", got)
	}
	want := "460MiB" // 0.9 * 512Mi ≈ 460.8 → 460Mi
	if rec.Recommended != want {
		t.Errorf("Recommended = %q, want %q", rec.Recommended, want)
	}
	if rec.Severity != SeverityWarning {
		t.Errorf("Severity = %q, want warning", rec.Severity)
	}
}

func TestGoAnalyzer_GOGC_silentWhenGCFine(t *testing.T) {
	cur := CurrentSettings{
		Env:       map[string]string{},
		GoVersion: "1.26.0",
	}
	obs := ObservedMetrics{GCPauseP99Ms: 2.0}
	got := GoAnalyzer{}.Analyze(obs, cur)
	if findRec(got, "env.GOGC") != nil {
		t.Error("expected no GOGC rec when GC pauses are low; got one")
	}
}

func TestGoAnalyzer_GOGC_suggestWhenGCStressed(t *testing.T) {
	cur := CurrentSettings{
		Env:       map[string]string{},
		GoVersion: "1.26.0",
	}
	obs := ObservedMetrics{GCPauseP99Ms: 75.0}
	got := GoAnalyzer{}.Analyze(obs, cur)
	rec := findRec(got, "env.GOGC")
	if rec == nil {
		t.Fatalf("expected GOGC rec when GC pauses are high; got none")
	}
	if rec.Severity != SeverityInfo {
		t.Errorf("Severity = %q, want info", rec.Severity)
	}
}

func findRec(recs []Recommendation, field string) *Recommendation {
	for i := range recs {
		if recs[i].Field == field {
			return &recs[i]
		}
	}
	return nil
}
