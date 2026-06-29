package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/siabroo/tuna/internal/analyzer"
)

// ContainerAnnotation lets users disambiguate which container to
// analyze in a multi-container pod.
const ContainerAnnotation = "tuna.siabroo.github.io/container"

// WorkloadAnnotation is the optional explicit override of detection.
// If set to e.g. "go", only the Go analyzer is tried.
const WorkloadAnnotation = "tuna.siabroo.github.io/workload"

// knownSidecars are container names skipped during ambiguity check.
var knownSidecars = map[string]struct{}{
	"istio-proxy":    {},
	"linkerd-proxy":  {},
	"vault-agent":    {},
	"envoy":          {},
	"otel-collector": {},
}

// PickContainer chooses the target container in a Deployment's pod template.
func PickContainer(dep *appsv1.Deployment) (string, error) {
	containers := dep.Spec.Template.Spec.Containers

	// User annotation always wins, if present.
	if name := dep.Annotations[ContainerAnnotation]; name != "" {
		for _, c := range containers {
			if c.Name == name {
				return name, nil
			}
		}
		return "", fmt.Errorf("annotation %s=%q but no container with that name in pod template", ContainerAnnotation, name)
	}

	if len(containers) == 0 {
		return "", fmt.Errorf("deployment has zero containers")
	}
	if len(containers) == 1 {
		return containers[0].Name, nil
	}

	// Multi-container: filter sidecars.
	var candidates []string
	for _, c := range containers {
		if _, isSidecar := knownSidecars[c.Name]; isSidecar {
			continue
		}
		candidates = append(candidates, c.Name)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return "", fmt.Errorf("ambiguous: multiple non-sidecar containers (%v); add annotation %s=<name>", candidates, ContainerAnnotation)
}

// envVarsOfInterest determines which env vars get copied into
// CurrentSettings.
var envVarsOfInterest = []string{"GOMAXPROCS", "GOMEMLIMIT", "GOGC", "GODEBUG"}

// ExtractCurrentSettings reads the analyzer-relevant fields of the
// named container from the Deployment pod template.
func ExtractCurrentSettings(dep *appsv1.Deployment, containerName string) (analyzer.CurrentSettings, error) {
	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name != containerName {
			continue
		}
		env := map[string]string{}
		for _, k := range envVarsOfInterest {
			env[k] = lookupEnv(c.Env, k)
		}
		return analyzer.CurrentSettings{
			Resources: c.Resources,
			Env:       env,
		}, nil
	}
	return analyzer.CurrentSettings{}, fmt.Errorf("container %q not found in pod template", containerName)
}

func lookupEnv(envs []corev1.EnvVar, name string) string {
	for _, e := range envs {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}
