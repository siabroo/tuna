package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition type names used in tuna's CR status.
const (
	ConditionReady            = "Ready"
	ConditionMetricsAvailable = "MetricsAvailable"
	ConditionWorkloadDetected = "WorkloadDetected"
	ConditionDataSufficient   = "DataSufficient"
)

// SetCondition adds or updates a condition in conds.
// LastTransitionTime is set only when the status changes.
func SetCondition(conds *[]metav1.Condition, condType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i, c := range *conds {
		if c.Type == condType {
			if c.Status != status {
				(*conds)[i].LastTransitionTime = now
			}
			(*conds)[i].Status = status
			(*conds)[i].Reason = reason
			(*conds)[i].Message = message
			return
		}
	}
	*conds = append(*conds, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}
