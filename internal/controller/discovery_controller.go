// Package controller wires tuna's two reconcilers (Discovery, Analysis)
// to the controller-runtime manager.
package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	tunav1alpha1 "github.com/siabroo/tuna/api/v1alpha1"
)

// AnalyzeAnnotation is the opt-in marker on a Deployment.
// Value "true" enables analysis; absence or any other value disables.
const AnalyzeAnnotation = "tuna.siabroo.github.io/analyze"

// DiscoveryReconciler watches Deployments and creates/deletes
// WorkloadRecommendation CRs based on the opt-in annotation.
type DiscoveryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// AddDiscoveryController is the manager.Runnable-style setup used both
// by main and by tests.
func AddDiscoveryController(mgr manager.Manager) error {
	r := &DiscoveryReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	return r.SetupWithManager(mgr)
}

// SetupWithManager registers the reconciler against Deployment events.
func (r *DiscoveryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}

// Reconcile handles a Deployment add/update/delete.
func (r *DiscoveryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("deployment", req.NamespacedName)

	dep := &appsv1.Deployment{}
	if err := r.Get(ctx, req.NamespacedName, dep); err != nil {
		if errors.IsNotFound(err) {
			// Deployment was deleted. The CR's ownerReference triggers
			// k8s GC automatically — nothing to do.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	wantsAnalysis := dep.Annotations[AnalyzeAnnotation] == "true"

	cr := &tunav1alpha1.WorkloadRecommendation{}
	crKey := types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}
	getErr := r.Get(ctx, crKey, cr)
	exists := getErr == nil
	if getErr != nil && !errors.IsNotFound(getErr) {
		return ctrl.Result{}, getErr
	}

	switch {
	case wantsAnalysis && !exists:
		newCR := &tunav1alpha1.WorkloadRecommendation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dep.Name,
				Namespace: dep.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					ownerRefFor(dep),
				},
			},
			Spec: tunav1alpha1.WorkloadRecommendationSpec{
				TargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       dep.Name,
					Namespace:  dep.Namespace,
					UID:        dep.UID,
				},
			},
		}
		if err := r.Create(ctx, newCR); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("created WorkloadRecommendation")
	case !wantsAnalysis && exists:
		// k8s GC only fires when the Deployment is deleted. If the user
		// just removed the annotation, we must delete explicitly.
		if err := r.Delete(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("deleted WorkloadRecommendation (annotation removed)")
	default:
		// states: (wants && exists) or (!wants && !exists) — nothing to do
	}

	return ctrl.Result{}, nil
}

// ownerRefFor builds an OwnerReference to the given Deployment with
// Controller=true and BlockOwnerDeletion=false (spec P1.2: blocking
// would require update/delete RBAC on Deployments).
func ownerRefFor(dep *appsv1.Deployment) metav1.OwnerReference {
	t := true
	f := false
	return metav1.OwnerReference{
		APIVersion:         "apps/v1",
		Kind:               "Deployment",
		Name:               dep.Name,
		UID:                dep.UID,
		Controller:         &t,
		BlockOwnerDeletion: &f,
	}
}
