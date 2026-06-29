package controller_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	tunav1alpha1 "github.com/siabroo/tuna/api/v1alpha1"
	"github.com/siabroo/tuna/internal/controller"
	"github.com/siabroo/tuna/internal/testenv"
)

func TestDiscovery_CreatesCROnAnnotation(t *testing.T) {
	env := testenv.Start(t)
	mgr := startManagerWith(t, env, controller.AddDiscoveryController)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = mgr.Start(ctx) }()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend",
			Namespace: "default",
			Annotations: map[string]string{
				controller.AnalyzeAnnotation: "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "backend"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "backend"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "app",
					Image: "ghcr.io/example/backend:latest",
					Ports: []corev1.ContainerPort{{ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
					ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt(8080)},
					}},
				}}},
			},
		},
	}
	if err := env.Client.Create(ctx, dep); err != nil {
		t.Fatalf("create Deployment: %v", err)
	}

	// Wait up to 5s for the controller to react.
	cr := &tunav1alpha1.WorkloadRecommendation{}
	if err := pollUntil(ctx, 5*time.Second, func() bool {
		err := env.Client.Get(ctx, types.NamespacedName{Name: "backend", Namespace: "default"}, cr)
		return err == nil
	}); err != nil {
		t.Fatalf("CR not created within 5s: %v", err)
	}

	// targetRef populated
	wantTarget := corev1.ObjectReference{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "backend",
		Namespace:  "default",
	}
	if diff := cmp.Diff(wantTarget.Name, cr.Spec.TargetRef.Name); diff != "" {
		t.Errorf("targetRef.Name mismatch (-want +got):\n%s", diff)
	}

	// ownerReference: controller=true, blockOwnerDeletion=false
	if len(cr.OwnerReferences) != 1 {
		t.Fatalf("ownerReferences len = %d, want 1", len(cr.OwnerReferences))
	}
	or := cr.OwnerReferences[0]
	if or.Kind != "Deployment" || or.Name != "backend" {
		t.Errorf("ownerRef = %+v, want Deployment/backend", or)
	}
	if or.Controller == nil || !*or.Controller {
		t.Error("ownerRef.Controller should be true")
	}
	if or.BlockOwnerDeletion != nil && *or.BlockOwnerDeletion {
		t.Error("ownerRef.BlockOwnerDeletion should be false (spec P1.2)")
	}

	// Silence unused
	_ = errors.IsNotFound
	_ = client.IgnoreNotFound
}

// pollUntil retries fn every 100ms until it returns true or timeout hits.
func pollUntil(ctx context.Context, timeout time.Duration, fn func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return context.DeadlineExceeded
}

// startManagerWith creates a manager, registers a setup function, and returns it.
func startManagerWith(t *testing.T, env *testenv.Env, setup func(manager.Manager) error) manager.Manager {
	t.Helper()
	mgr, err := manager.New(env.Config, manager.Options{Scheme: env.Client.Scheme()})
	if err != nil {
		t.Fatalf("manager.New: %v", err)
	}
	if err := setup(mgr); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return mgr
}
