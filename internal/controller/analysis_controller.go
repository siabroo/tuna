package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"github.com/siabroo/tuna/internal/selector"
)

const analysisWindow = 24 * time.Hour
const gcPauseWindow = 1 * time.Hour
const minSampleCoveragePct = 50

// AnalysisReconciler watches WorkloadRecommendation CRs, runs analysis,
// updates status.
type AnalysisReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Prom             *prom.Client
	Analyzers        []analyzer.Analyzer
	AnalysisInterval time.Duration
	Recorder         *DedupRecorder
}

// AddAnalysisController is the production setup used by main.
func AddAnalysisController(mgr manager.Manager, p *prom.Client, analyzers []analyzer.Analyzer, interval time.Duration) error {
	r := &AnalysisReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Prom:             p,
		Analyzers:        analyzers,
		AnalysisInterval: interval,
		Recorder:         NewDedupRecorder(mgr.GetEventRecorderFor("tuna-manager"), 5*time.Minute),
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
			ctrl.Log.Error(err, "deploymentToCR: failed to fetch WorkloadRecommendation", "key", key)
			return nil
		}
		return []reconcile.Request{{NamespacedName: key}}
	}
}

// Reconcile per spec §5.2.
func (r *AnalysisReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cr := &tunav1alpha1.WorkloadRecommendation{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch target Deployment via spec.targetRef.
	dep := &appsv1.Deployment{}
	depKey := types.NamespacedName{Name: cr.Spec.TargetRef.Name, Namespace: cr.Spec.TargetRef.Namespace}
	if err := r.Get(ctx, depKey, dep); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// Pick container.
	container, err := PickContainer(dep)
	if err != nil {
		SetCondition(&cr.Status.Conditions, ConditionWorkloadDetected, metav1.ConditionFalse, "AmbiguousContainer", err.Error())
		SetCondition(&cr.Status.Conditions, ConditionReady, metav1.ConditionFalse, "AmbiguousContainer", err.Error())
		if r.Recorder != nil {
			r.Recorder.Event(dep, "Warning", "WorkloadAmbiguous", err.Error())
		}
		return r.patchStatus(ctx, cr, ctrl.Result{RequeueAfter: 1 * time.Hour})
	}

	// Resolve pods.
	pods, err := selector.ResolvePods(ctx, r.Client, dep)
	if err != nil {
		SetCondition(&cr.Status.Conditions, ConditionReady, metav1.ConditionFalse, "PodListFailed", err.Error())
		markConditionsUnknown(&cr.Status.Conditions, "PodListFailed", "cannot verify metrics/workload while pod list failed",
			ConditionMetricsAvailable, ConditionWorkloadDetected, ConditionDataSufficient)
		return r.patchStatus(ctx, cr, ctrl.Result{RequeueAfter: 1 * time.Minute})
	}
	if len(pods) == 0 {
		SetCondition(&cr.Status.Conditions, ConditionDataSufficient, metav1.ConditionFalse, "NoRunningPods", "deployment has zero pods")
		SetCondition(&cr.Status.Conditions, ConditionReady, metav1.ConditionFalse, "NoRunningPods", "deployment has zero pods")
		markConditionsUnknown(&cr.Status.Conditions, "NoRunningPods", "no pods running",
			ConditionMetricsAvailable, ConditionWorkloadDetected)
		return r.patchStatus(ctx, cr, ctrl.Result{RequeueAfter: 5 * time.Minute})
	}
	podRegex := selector.PodRegex(pods)

	// Detect workload.
	a, err := selector.DetectWorkload(ctx, r.Prom, r.Analyzers, dep.Namespace, podRegex)
	if err != nil {
		SetCondition(&cr.Status.Conditions, ConditionMetricsAvailable, metav1.ConditionFalse, "PrometheusUnreachable", err.Error())
		SetCondition(&cr.Status.Conditions, ConditionReady, metav1.ConditionFalse, "PrometheusUnreachable", err.Error())
		markConditionsUnknown(&cr.Status.Conditions, "PrometheusUnreachable", "cannot verify while Prometheus is unreachable",
			ConditionWorkloadDetected, ConditionDataSufficient)
		if r.Recorder != nil {
			r.Recorder.Event(dep, "Warning", "MetricsUnavailable",
				fmt.Sprintf("Failed to query Prometheus: %v", err))
		}
		return r.patchStatus(ctx, cr, ctrl.Result{RequeueAfter: 1 * time.Minute})
	}
	if a == nil {
		SetCondition(&cr.Status.Conditions, ConditionWorkloadDetected, metav1.ConditionFalse, "NoAnalyzerMatched", "no registered analyzer matched (no go_info series for this deployment)")
		SetCondition(&cr.Status.Conditions, ConditionReady, metav1.ConditionFalse, "NoAnalyzerMatched", "no registered analyzer matched")
		return r.patchStatus(ctx, cr, ctrl.Result{RequeueAfter: 1 * time.Hour})
	}
	SetCondition(&cr.Status.Conditions, ConditionWorkloadDetected, metav1.ConditionTrue, "Detected", fmt.Sprintf("analyzer %s matched", a.Name()))
	SetCondition(&cr.Status.Conditions, ConditionMetricsAvailable, metav1.ConditionTrue, "PrometheusReachable", "PromQL endpoint reachable")
	if r.Recorder != nil {
		r.Recorder.Event(dep, "Normal", "WorkloadDetected",
			fmt.Sprintf("Detected %s runtime via go_info series", a.Name()))
	}

	// Read current settings.
	cur, err := ExtractCurrentSettings(dep, container)
	if err != nil {
		SetCondition(&cr.Status.Conditions, ConditionReady, metav1.ConditionFalse, "ExtractFailed", err.Error())
		return r.patchStatus(ctx, cr, ctrl.Result{RequeueAfter: 5 * time.Minute})
	}

	// Best-effort: extract Go version from the go_info series' "version" label.
	// If extraction fails, GoVersion stays empty and the analyzer treats as unknown.
	if version, _, err := r.Prom.QueryFirstLabel(ctx, prom.QueryGoVersion(dep.Namespace, podRegex), "version"); err == nil {
		cur.GoVersion = version
	}

	// Query metrics serially.
	obs := analyzer.ObservedMetrics{Window: "24h", GCPauseP99Window: "1h"}
	{
		v, _, qerr := r.Prom.Query(ctx, prom.QueryCPUPercentile(dep.Namespace, podRegex, container, 0.50, analysisWindow))
		if qerr == nil {
			obs.CPUMillicoresP50 = int64(v)
		}
	}
	{
		v, _, qerr := r.Prom.Query(ctx, prom.QueryCPUPercentile(dep.Namespace, podRegex, container, 0.95, analysisWindow))
		if qerr == nil {
			obs.CPUMillicoresP95 = int64(v)
		}
	}
	{
		v, _, qerr := r.Prom.Query(ctx, prom.QueryCPUPercentile(dep.Namespace, podRegex, container, 0.99, analysisWindow))
		if qerr == nil {
			obs.CPUMillicoresP99 = int64(v)
		}
	}
	{
		v, _, qerr := r.Prom.Query(ctx, prom.QueryMemoryWorkingSetPercentile(dep.Namespace, podRegex, container, 0.95, analysisWindow))
		if qerr == nil {
			obs.MemoryWorkingSetP95Bytes = int64(v)
		}
	}
	{
		v, _, qerr := r.Prom.Query(ctx, prom.QueryMemoryWorkingSetPercentile(dep.Namespace, podRegex, container, 0.99, analysisWindow))
		if qerr == nil {
			obs.MemoryWorkingSetP99Bytes = int64(v)
		}
	}
	{
		v, _, qerr := r.Prom.Query(ctx, prom.QueryGCPauseP99(dep.Namespace, podRegex, gcPauseWindow))
		if qerr == nil {
			obs.GCPauseP99Ms = v
		}
	}

	// Data sufficiency: if all CPU + memory percentiles are zero, data is insufficient.
	if obs.CPUMillicoresP95 == 0 && obs.MemoryWorkingSetP95Bytes == 0 {
		SetCondition(&cr.Status.Conditions, ConditionDataSufficient, metav1.ConditionFalse, "InsufficientData", "no CPU or memory samples in analysis window")
	} else {
		SetCondition(&cr.Status.Conditions, ConditionDataSufficient, metav1.ConditionTrue, "SamplesAdequate", fmt.Sprintf(">=%d%% of %s window covered", minSampleCoveragePct, analysisWindow))
	}

	// Run analyzers.
	var recs []analyzer.Recommendation
	recs = append(recs, a.Analyze(obs, cur)...)

	// Emit AnalysisCompleted event.
	if r.Recorder != nil {
		warnings := 0
		infos := 0
		for _, rec := range recs {
			switch rec.Severity {
			case analyzer.SeverityWarning:
				warnings++
			case analyzer.SeverityInfo:
				infos++
			}
		}
		r.Recorder.Event(dep, "Normal", "AnalysisCompleted",
			fmt.Sprintf("Generated %d recommendations (%d warning, %d info)", len(recs), warnings, infos))
	}

	// Write status.
	cr.Status.LastAnalyzedAt = &metav1.Time{Time: time.Now()}
	cr.Status.Workload = &tunav1alpha1.WorkloadInfo{Type: a.Name()}
	cr.Status.Observed = observedToCRD(obs)
	cr.Status.Current = currentToCRD(cur)
	cr.Status.Recommendations = recommendationsToCRD(recs)
	cr.Status.RecommendationCount = len(cr.Status.Recommendations)

	SetCondition(&cr.Status.Conditions, ConditionReady, metav1.ConditionTrue, "AnalysisFresh", "analysis completed")
	return r.patchStatus(ctx, cr, ctrl.Result{RequeueAfter: r.AnalysisInterval})
}

// markConditionsUnknown sets the listed conditions to Unknown with the given
// reason, ensuring no prior-success conditions linger on a partial failure.
func markConditionsUnknown(conds *[]metav1.Condition, reason, message string, types ...string) {
	for _, t := range types {
		SetCondition(conds, t, metav1.ConditionUnknown, reason, message)
	}
}

// patchStatus writes cr.Status back to the cluster, then returns the
// requested ctrl.Result + nil error.
func (r *AnalysisReconciler) patchStatus(ctx context.Context, cr *tunav1alpha1.WorkloadRecommendation, res ctrl.Result) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}
	return res, nil
}

func observedToCRD(o analyzer.ObservedMetrics) *tunav1alpha1.ObservedMetrics {
	out := &tunav1alpha1.ObservedMetrics{Window: o.Window}
	if o.CPUMillicoresP95 > 0 || o.CPUMillicoresP99 > 0 {
		out.CPU = &tunav1alpha1.CPUMetrics{
			P50Millicores: o.CPUMillicoresP50,
			P95Millicores: o.CPUMillicoresP95,
			P99Millicores: o.CPUMillicoresP99,
		}
	}
	if o.MemoryWorkingSetP95Bytes > 0 || o.MemoryWorkingSetP99Bytes > 0 {
		out.Memory = &tunav1alpha1.MemoryMetrics{
			WorkingSetP95Bytes: o.MemoryWorkingSetP95Bytes,
			WorkingSetP99Bytes: o.MemoryWorkingSetP99Bytes,
		}
	}
	if o.GCPauseP99Ms > 0 {
		out.GC = &tunav1alpha1.GCMetrics{
			PauseP99Ms:     o.GCPauseP99Ms,
			PauseP99Window: o.GCPauseP99Window,
		}
	}
	return out
}

func currentToCRD(c analyzer.CurrentSettings) *tunav1alpha1.CurrentSettings {
	res := c.Resources // value copy
	return &tunav1alpha1.CurrentSettings{
		Resources: &res,
		Env:       c.Env,
	}
}

func recommendationsToCRD(recs []analyzer.Recommendation) []tunav1alpha1.Recommendation {
	out := make([]tunav1alpha1.Recommendation, len(recs))
	for i, rec := range recs {
		out[i] = tunav1alpha1.Recommendation{
			Field:       rec.Field,
			Current:     rec.Current,
			Recommended: rec.Recommended,
			Rationale:   rec.Rationale,
			Severity:    rec.Severity,
		}
	}
	return out
}
