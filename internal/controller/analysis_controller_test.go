package controller_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunav1alpha1 "github.com/siabroo/tuna/api/v1alpha1"
	"github.com/siabroo/tuna/internal/analyzer"
	"github.com/siabroo/tuna/internal/controller"
	"github.com/siabroo/tuna/internal/prom"
	"github.com/siabroo/tuna/internal/testenv"
)

func TestAnalysis_ReconciledOnDeploymentChange(t *testing.T) {
	env := testenv.Start(t)

	var reconcileCount int32
	stub := reconcile.Func(func(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
		atomic.AddInt32(&reconcileCount, 1)
		return ctrl.Result{}, nil
	})

	mgr := startManagerWith(t, env, func(m manager.Manager) error {
		return controller.SetupAnalysisControllerWithReconciler(m, stub)
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = mgr.Start(ctx) }()

	// Create CR first (would normally be created by DiscoveryController).
	cr := &tunav1alpha1.WorkloadRecommendation{
		ObjectMeta: metav1.ObjectMeta{Name: "tgt", Namespace: "default"},
		Spec: tunav1alpha1.WorkloadRecommendationSpec{
			TargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1", Kind: "Deployment",
				Name: "tgt", Namespace: "default",
			},
		},
	}
	if err := env.Client.Create(ctx, cr); err != nil {
		t.Fatalf("create CR: %v", err)
	}

	// Wait for the initial reconcile from the CR creation.
	if err := pollUntil(ctx, 5*time.Second, func() bool {
		return atomic.LoadInt32(&reconcileCount) >= 1
	}); err != nil {
		t.Fatalf("no initial reconcile: %v", err)
	}
	initial := atomic.LoadInt32(&reconcileCount)

	// Now create a Deployment with the same name → should trigger reconcile.
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "tgt", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "tgt"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "tgt"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}},
			},
		},
	}
	if err := env.Client.Create(ctx, dep); err != nil {
		t.Fatalf("create dep: %v", err)
	}

	if err := pollUntil(ctx, 5*time.Second, func() bool {
		return atomic.LoadInt32(&reconcileCount) > initial
	}); err != nil {
		t.Fatalf("Deployment change did not trigger reconcile: %v", err)
	}

}

func TestAnalysis_WritesStatusWithRecommendations(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mock := testenv.StartPromMock(t, func(query string) testenv.PromResult {
		switch {
		case containsAll(query, "count(go_info"):
			return testenv.PromResult{Value: 1.0}
		case containsAll(query, "container_cpu_usage_seconds_total", "0.50"):
			return testenv.PromResult{Value: 120.0} // p50 = 120m
		case containsAll(query, "container_cpu_usage_seconds_total", "0.95"):
			return testenv.PromResult{Value: 340.0} // p95 = 340m
		case containsAll(query, "container_cpu_usage_seconds_total", "0.99"):
			return testenv.PromResult{Value: 510.0} // p99 = 510m
		case containsAll(query, "container_memory_working_set_bytes", "0.95"):
			return testenv.PromResult{Value: 398458880}
		default:
			return testenv.PromResult{Empty: true}
		}
	})

	pClient, _ := prom.NewClient(mock.URL, prom.AuthNone)

	mgr := startManagerWith(t, env, func(m manager.Manager) error {
		if err := controller.AddDiscoveryController(m); err != nil {
			return err
		}
		return controller.AddAnalysisController(m, pClient,
			[]analyzer.Analyzer{analyzer.GoAnalyzer{}},
			5*time.Second,
		)
	})
	go func() { _ = mgr.Start(ctx) }()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc", Namespace: "default",
			Annotations: map[string]string{controller.AnalyzeAnnotation: "true"},
		},
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
	// Need a pod for ResolvePods.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-0", Namespace: "default", Labels: map[string]string{"app": "svc"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}},
	}
	if err := env.Client.Create(ctx, pod); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	if err := pollUntil(ctx, 30*time.Second, func() bool {
		cr := &tunav1alpha1.WorkloadRecommendation{}
		if err := env.Client.Get(ctx, types.NamespacedName{Name: "svc", Namespace: "default"}, cr); err != nil {
			return false
		}
		return len(cr.Status.Recommendations) > 0
	}); err != nil {
		t.Fatalf("no recommendations written within 30s: %v", err)
	}

	cr := &tunav1alpha1.WorkloadRecommendation{}
	_ = env.Client.Get(ctx, types.NamespacedName{Name: "svc", Namespace: "default"}, cr)

	if cr.Status.Workload == nil || cr.Status.Workload.Type != "go" {
		t.Errorf("Workload.Type = %v, want go", cr.Status.Workload)
	}
	if !hasConditionTrue(cr.Status.Conditions, controller.ConditionReady) {
		t.Errorf("Ready condition not True; got: %+v", cr.Status.Conditions)
	}

	// At least one recommendation must be for CPU requests (non-zero mock values ensure non-zero obs).
	hasCPURec := false
	for _, rec := range cr.Status.Recommendations {
		if rec.Field == "resources.requests.cpu" {
			hasCPURec = true
			break
		}
	}
	if !hasCPURec {
		t.Errorf("no recommendation with Field=resources.requests.cpu; got recs: %+v", cr.Status.Recommendations)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func hasConditionTrue(conds []metav1.Condition, condType string) bool {
	for _, c := range conds {
		if c.Type == condType && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
