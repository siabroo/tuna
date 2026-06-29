package selector_test

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/siabroo/tuna/internal/analyzer"
	"github.com/siabroo/tuna/internal/prom"
	"github.com/siabroo/tuna/internal/selector"
	"github.com/siabroo/tuna/internal/testenv"
)

func TestResolvePods_MatchesByDeploymentSelector(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "svc"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "svc"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}},
			},
		},
	}
	if err := env.Client.Create(ctx, dep); err != nil {
		t.Fatalf("create dep: %v", err)
	}

	// Add some pods that should and shouldn't match.
	for _, p := range []*corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "svc-aaa", Namespace: "default", Labels: map[string]string{"app": "svc"}},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "svc-bbb", Namespace: "default", Labels: map[string]string{"app": "svc"}},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default", Labels: map[string]string{"app": "other"}},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}}},
	} {
		if err := env.Client.Create(ctx, p); err != nil {
			t.Fatalf("create pod %s: %v", p.Name, err)
		}
	}

	pods, err := selector.ResolvePods(ctx, env.Client, dep)
	if err != nil {
		t.Fatalf("ResolvePods: %v", err)
	}
	want := map[string]bool{"svc-aaa": true, "svc-bbb": true}
	if len(pods) != 2 {
		t.Fatalf("got %d pods, want 2: %v", len(pods), pods)
	}
	for _, p := range pods {
		if !want[p] {
			t.Errorf("unexpected pod %q in result", p)
		}
	}
}

func TestPodRegex(t *testing.T) {
	tests := []struct {
		pods []string
		want string
	}{
		{[]string{"a"}, `^(a)$`},
		{[]string{"a", "b"}, `^(a|b)$`},
		{[]string{}, `^()$`},
	}
	for _, tc := range tests {
		got := selector.PodRegex(tc.pods)
		if !strings.HasPrefix(got, "^(") || !strings.HasSuffix(got, ")$") {
			t.Errorf("PodRegex(%v) = %q, missing anchors", tc.pods, got)
		}
		if got != tc.want {
			t.Errorf("PodRegex(%v) = %q, want %q", tc.pods, got, tc.want)
		}
	}
}

func TestDetectWorkload_GoMatched(t *testing.T) {
	mock := testenv.StartPromMock(t, func(query string) testenv.PromResult {
		if strings.Contains(query, "go_info") {
			return testenv.PromResult{Value: 1.0}
		}
		return testenv.PromResult{Empty: true}
	})

	client, _ := prom.NewClient(mock.URL, prom.AuthNone)
	a, err := selector.DetectWorkload(context.Background(), client,
		[]analyzer.Analyzer{analyzer.GoAnalyzer{}},
		"default", "^(svc-aaa)$",
	)
	if err != nil {
		t.Fatalf("DetectWorkload: %v", err)
	}
	if a == nil || a.Name() != "go" {
		t.Errorf("expected GoAnalyzer, got %v", a)
	}
}

func TestDetectWorkload_NoMatch(t *testing.T) {
	mock := testenv.StartPromMock(t, func(query string) testenv.PromResult {
		return testenv.PromResult{Empty: true}
	})
	client, _ := prom.NewClient(mock.URL, prom.AuthNone)
	a, err := selector.DetectWorkload(context.Background(), client,
		[]analyzer.Analyzer{analyzer.GoAnalyzer{}},
		"default", "^(any)$",
	)
	if err != nil {
		t.Fatalf("DetectWorkload: %v", err)
	}
	if a != nil {
		t.Errorf("expected nil analyzer, got %v", a)
	}
}
