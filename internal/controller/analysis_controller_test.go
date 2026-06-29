package controller_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunav1alpha1 "github.com/siabroo/tuna/api/v1alpha1"
	"github.com/siabroo/tuna/internal/controller"
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
