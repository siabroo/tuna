package controller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tunav1alpha1 "github.com/siabroo/tuna/api/v1alpha1"
	"github.com/siabroo/tuna/internal/analyzer"
	"github.com/siabroo/tuna/internal/prom"
)

// AnalysisReconciler watches WorkloadRecommendation CRs, runs analysis,
// updates status.
type AnalysisReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Prom             *prom.Client
	Analyzers        []analyzer.Analyzer
	AnalysisInterval time.Duration
}

// AddAnalysisController is the production setup used by main.
func AddAnalysisController(mgr manager.Manager, p *prom.Client, analyzers []analyzer.Analyzer, interval time.Duration) error {
	r := &AnalysisReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Prom:             p,
		Analyzers:        analyzers,
		AnalysisInterval: interval,
	}
	return SetupAnalysisControllerWithReconciler(mgr, r)
}

// SetupAnalysisControllerWithReconciler wires the watches against a
// caller-provided reconcile.Reconciler. Used by tests to substitute
// a stub reconciler.
func SetupAnalysisControllerWithReconciler(mgr manager.Manager, r reconcile.Reconciler) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tunav1alpha1.WorkloadRecommendation{}).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(deploymentToCR(mgr.GetClient())),
		).
		Complete(r)
}

// deploymentToCR maps a Deployment event to its corresponding
// WorkloadRecommendation reconcile request.
func deploymentToCR(c client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		key := types.NamespacedName{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		}
		cr := &tunav1alpha1.WorkloadRecommendation{}
		if err := c.Get(ctx, key, cr); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return nil
		}
		return []reconcile.Request{{NamespacedName: key}}
	}
}

// Reconcile is the real reconcile (no-op stub until Task 13).
func (r *AnalysisReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{RequeueAfter: r.AnalysisInterval}, nil
}
