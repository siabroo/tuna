// Package selector resolves a Deployment to the set of pods it
// currently owns, and builds the PromQL pod=~"..." regex fragment.
package selector

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/siabroo/tuna/internal/analyzer"
	"github.com/siabroo/tuna/internal/prom"
)

// ResolvePods lists pods in dep.Namespace matching dep.Spec.Selector.
// Returns pod names sorted for determinism. Empty list is a valid
// result (e.g., Deployment scaled to 0).
func ResolvePods(ctx context.Context, c client.Client, dep *appsv1.Deployment) ([]string, error) {
	if dep.Spec.Selector == nil {
		return nil, fmt.Errorf("selector: deployment %s/%s has nil spec.selector", dep.Namespace, dep.Name)
	}
	sel, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("selector: invalid label selector: %w", err)
	}
	if sel.Empty() {
		return nil, fmt.Errorf("selector: empty selector matches everything; refusing")
	}

	var podList corev1.PodList
	if err := c.List(ctx, &podList,
		client.InNamespace(dep.Namespace),
		client.MatchingLabelsSelector{Selector: sel},
	); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(podList.Items))
	for _, p := range podList.Items {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names, nil
}

// PodRegex builds a PromQL pod-name regex from the given pods.
// Output is anchored with ^( ... )$.
// Empty input yields "^()$", which matches nothing.
func PodRegex(pods []string) string {
	var b strings.Builder
	b.WriteString("^(")
	for i, p := range pods {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(p)
	}
	b.WriteString(")$")
	return b.String()
}

// DetectWorkload runs each analyzer's PromQL detection probe with the
// given pod selector. First analyzer whose probe returns a nonzero
// scalar wins. Returns (nil, nil) if no probe matched.
func DetectWorkload(
	ctx context.Context,
	p *prom.Client,
	analyzers []analyzer.Analyzer,
	namespace, podRegex string,
) (analyzer.Analyzer, error) {
	labelClause := fmt.Sprintf(`namespace=%q,pod=~%q`, namespace, podRegex)
	for _, a := range analyzers {
		q := fmt.Sprintf(a.DetectionProbe(), labelClause)
		v, empty, err := p.Query(ctx, q)
		if err != nil {
			return nil, err
		}
		if !empty && v > 0 {
			return a, nil
		}
	}
	return nil, nil
}
