//go:build e2e

package e2e_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tunav1alpha1 "github.com/siabroo/tuna/api/v1alpha1"
)

func TestE2E_WorkloadRecommendationProduced(t *testing.T) {
	// Find kubeconfig — use kind cluster's default context.
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		t.Fatalf("client config: %v", err)
	}

	if err := tunav1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Minute)
	defer cancel()

	// Sanity: tuna-manager pod should be Ready.
	if !waitForPodReady(t, "tuna-system", "app.kubernetes.io/name=tuna", 2*time.Minute) {
		t.Fatal("tuna-manager pod did not become Ready")
	}

	// Sanity: loadgen pods Ready.
	if !waitForPodReady(t, "default", "app=loadgen", 2*time.Minute) {
		t.Fatal("loadgen pods did not become Ready")
	}

	// Wait for WorkloadRecommendation to appear with recommendations.
	cr := &tunav1alpha1.WorkloadRecommendation{}
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		err := cl.Get(ctx, types.NamespacedName{Name: "loadgen", Namespace: "default"}, cr)
		if err == nil && len(cr.Status.Recommendations) > 0 {
			break
		}
		time.Sleep(30 * time.Second)
	}
	if len(cr.Status.Recommendations) == 0 {
		t.Fatalf("no recommendations produced within 8min; CR: %+v", cr.Status)
	}

	// Assert Ready condition.
	if !hasConditionTrue(cr.Status.Conditions, "Ready") {
		t.Errorf("Ready condition not True; got: %+v", cr.Status.Conditions)
	}
	if cr.Status.Workload == nil || cr.Status.Workload.Type != "go" {
		t.Errorf("Workload.Type = %v, want go", cr.Status.Workload)
	}

	t.Logf("E2E success: %d recommendations on loadgen", len(cr.Status.Recommendations))
}

func waitForPodReady(t *testing.T, namespace, labelSelector string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("kubectl", "wait", "--for=condition=Ready",
			"-n", namespace, "pod", "-l", labelSelector, "--timeout=5s").CombinedOutput()
		if err == nil {
			return true
		}
		if !strings.Contains(string(out), "timeout") {
			t.Logf("kubectl wait: %s", string(out))
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

func hasConditionTrue(conds []metav1.Condition, t string) bool {
	for _, c := range conds {
		if c.Type == t && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

