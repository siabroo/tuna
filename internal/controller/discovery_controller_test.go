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
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
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
	got := cr.Spec.TargetRef
	got.UID = "" // UID is env-assigned; exclude from structural check
	if diff := cmp.Diff(wantTarget, got); diff != "" {
		t.Errorf("targetRef mismatch (-want +got):\n%s", diff)
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
// SkipNameValidation is set so that multiple test managers in the same process
// can each register a controller with the same type-derived name without error.
func startManagerWith(t *testing.T, env *testenv.Env, setup func(manager.Manager) error) manager.Manager {
	t.Helper()
	skip := true
	mgr, err := manager.New(env.Config, manager.Options{
		Scheme:     env.Client.Scheme(),
		Controller: ctrlconfig.Controller{SkipNameValidation: &skip},
	})
	if err != nil {
		t.Fatalf("manager.New: %v", err)
	}
	if err := setup(mgr); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return mgr
}

func TestDiscovery_DeletesCROnAnnotationRemoval(t *testing.T) {
	env := testenv.Start(t)
	mgr := startManagerWith(t, env, controller.AddDiscoveryController)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = mgr.Start(ctx) }()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend2",
			Namespace: "default",
			Annotations: map[string]string{
				controller.AnalyzeAnnotation: "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "backend2"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "backend2"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}},
			},
		},
	}
	if err := env.Client.Create(ctx, dep); err != nil {
		t.Fatalf("create Deployment: %v", err)
	}

	// Wait for CR to appear (Task 4 behavior).
	if err := pollUntil(ctx, 5*time.Second, func() bool {
		cr := &tunav1alpha1.WorkloadRecommendation{}
		return env.Client.Get(ctx, types.NamespacedName{Name: "backend2", Namespace: "default"}, cr) == nil
	}); err != nil {
		t.Fatalf("CR not created within 5s: %v", err)
	}

	// Remove the annotation.
	if err := env.Client.Get(ctx, types.NamespacedName{Name: "backend2", Namespace: "default"}, dep); err != nil {
		t.Fatalf("re-fetch dep: %v", err)
	}
	delete(dep.Annotations, controller.AnalyzeAnnotation)
	if err := env.Client.Update(ctx, dep); err != nil {
		t.Fatalf("update dep: %v", err)
	}

	// CR should be gone within 5s.
	if err := pollUntil(ctx, 5*time.Second, func() bool {
		cr := &tunav1alpha1.WorkloadRecommendation{}
		err := env.Client.Get(ctx, types.NamespacedName{Name: "backend2", Namespace: "default"}, cr)
		return errors.IsNotFound(err)
	}); err != nil {
		t.Fatalf("CR still present after annotation removal: %v", err)
	}
}
