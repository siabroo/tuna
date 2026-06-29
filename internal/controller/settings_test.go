package controller_test

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/siabroo/tuna/internal/controller"
)

func TestPickContainer(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		containers  []corev1.Container
		want        string
		wantErr     bool
	}{
		{
			name: "single container",
			containers: []corev1.Container{
				{Name: "app"},
			},
			want: "app",
		},
		{
			name: "multi, annotation picks one",
			annotations: map[string]string{
				"tuna.siabroo.github.io/container": "app",
			},
			containers: []corev1.Container{
				{Name: "app"},
				{Name: "extra"},
			},
			want: "app",
		},
		{
			name: "multi, sidecar (istio-proxy) skipped → unambiguous",
			containers: []corev1.Container{
				{Name: "istio-proxy"},
				{Name: "app"},
			},
			want: "app",
		},
		{
			name: "multi, no hint → ambiguous error",
			containers: []corev1.Container{
				{Name: "app"},
				{Name: "secondary"},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Annotations: tc.annotations},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: tc.containers},
					},
				},
			}
			got, err := controller.PickContainer(dep)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (picked %q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("PickContainer = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractCurrentSettings(t *testing.T) {
	dep := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{
					{
						Name: "app",
						Env: []corev1.EnvVar{
							{Name: "GOMAXPROCS", Value: "2"},
							{Name: "GOMEMLIMIT", Value: "256MiB"},
							{Name: "OTHER", Value: "ignored"},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				}},
			},
		},
	}
	got, err := controller.ExtractCurrentSettings(dep, "app")
	if err != nil {
		t.Fatalf("ExtractCurrentSettings: %v", err)
	}
	if got.Env["GOMAXPROCS"] != "2" {
		t.Errorf("Env[GOMAXPROCS] = %q, want 2", got.Env["GOMAXPROCS"])
	}
	if got.Env["GOMEMLIMIT"] != "256MiB" {
		t.Errorf("Env[GOMEMLIMIT] = %q, want 256MiB", got.Env["GOMEMLIMIT"])
	}
	// OTHER should not be in the map (we filter to only relevant keys).
	if _, ok := got.Env["OTHER"]; ok {
		t.Error("Env[OTHER] should be filtered out")
	}
	if v := got.Resources.Requests.Cpu().MilliValue(); v != 100 {
		t.Errorf("Requests.Cpu = %dm, want 100", v)
	}
}
