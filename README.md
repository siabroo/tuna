# tuna

> A read-only Kubernetes operator that recommends right-sized resource limits, `GOMAXPROCS`, `GOMEMLIMIT`, and `GOGC` for Go workloads — based on Prometheus metrics.

[![CI](https://github.com/siabroo/tuna/actions/workflows/test.yml/badge.svg)](https://github.com/siabroo/tuna/actions/workflows/test.yml)
[![Lint](https://github.com/siabroo/tuna/actions/workflows/lint.yml/badge.svg)](https://github.com/siabroo/tuna/actions/workflows/lint.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/siabroo/tuna)](https://goreportcard.com/report/github.com/siabroo/tuna)

## What it does

You add one annotation to your Go Deployment:

```yaml
metadata:
  annotations:
    tuna.siabroo.github.io/analyze: "true"
```

tuna picks it up, queries Prometheus for runtime + container metrics, and creates a `WorkloadRecommendation` CR with right-sizing advice — for example:

```yaml
status:
  workload: { type: Go, version: "1.26.0" }
  recommendations:
    - field: resources.requests.cpu
      current: "100m"
      recommended: "350m"
      rationale: "p95 CPU usage 340m exceeds requests 100m; scheduler will mispack the pod."
      severity: warning
    - field: env.GOMEMLIMIT
      current: "(unset)"
      recommended: "460MiB"
      rationale: "GC pacing improves when GOMEMLIMIT ≈ 0.9 × container memory limit."
      severity: warning
```

**No mutation.** tuna never patches your Deployment. You read the advice and decide.

## Why not just use [...]?

| | tuna | [Robusta KRR](https://github.com/robusta-dev/krr) | [Goldilocks](https://github.com/FairwindsOps/goldilocks) | [VPA](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler) |
|---|---|---|---|---|
| Form factor | in-cluster operator | CLI tool | dashboard | controller |
| Workload-aware (Go GC, GOMAXPROCS) | ✓ | ✗ | ✗ | ✗ |
| Auto-discovery via annotation | ✓ | ✗ | namespace label | annotation/label |
| Read-only by design | ✓ | ✓ | ✓ | ✗ (mutates) |
| Multi-runtime (Go, Node, JVM) | Go (Node planned) | container-level only | container-level only | container-level only |
| Pluggable Prometheus storage | ✓ (kube-prom, GMP) | ✓ | requires VPA install | ✗ |

tuna fills the "in-cluster, workload-aware, read-only" intersection.

## Quickstart (local with kind)

```bash
# 1. Create cluster + kube-prometheus-stack
kind create cluster
helm install kube-prom prometheus-community/kube-prometheus-stack \
  --namespace monitoring --create-namespace

# 2. Install tuna
kubectl apply -f https://github.com/siabroo/tuna/releases/latest/download/install.yaml

# 3. Annotate your Deployment
kubectl annotate deployment my-go-app tuna.siabroo.github.io/analyze=true

# 4. Read the recommendation (wait ~5 min for metrics)
kubectl describe workloadrecommendation my-go-app
```

## Installation

### Local (kind, minikube, Docker Desktop)

```bash
kubectl apply -f https://github.com/siabroo/tuna/releases/latest/download/install.yaml
```

The default config expects Prometheus at `http://prometheus-operated.monitoring.svc:9090` (matches kube-prometheus-stack). To override, edit the Deployment's `PROMETHEUS_URL` env var.

### GCP (GKE + Google Managed Prometheus)

```bash
# Enable GMP on your cluster
gcloud container clusters update YOUR-CLUSTER --enable-managed-prometheus

# Install tuna
kubectl apply -f https://github.com/siabroo/tuna/releases/latest/download/install.yaml

# Configure auth + endpoint
kubectl set env deployment/tuna-manager -n tuna-system \
  PROMETHEUS_URL=https://monitoring.googleapis.com/v1/projects/$PROJECT/location/global/prometheus \
  AUTH_MODE=gcp-id-token
```

You'll need Workload Identity bound to a GCP service account with `roles/monitoring.viewer`. See [`docs/cloud-acceptance.md`](docs/cloud-acceptance.md) for the full procedure.

### AWS (EKS + AMP)

Phase 2 — `sigv4` auth mode is not in the first release. See [issue tracker] for status.

## How it works

1. **DiscoveryController** watches Deployments. On annotation `tuna.siabroo.github.io/analyze=true` it creates a `WorkloadRecommendation` CR.
2. **AnalysisController** watches CRs. On each reconcile:
   - Resolves the Deployment's pod set via k8s API.
   - Detects workload type by probing for `go_info` series in Prometheus.
   - Reads current `resources` + relevant env vars (`GOMAXPROCS`, `GOMEMLIMIT`, `GOGC`) from the Deployment.
   - Queries Prometheus for CPU/memory/GC-pause percentiles over a 24h window.
   - Runs the appropriate analyzer (currently `GoAnalyzer`).
   - Writes structured recommendations + conditions to `status`.
3. **You read** the recommendations and decide whether to apply them.

See [`docs/superpowers/specs/2026-06-29-tuna-design.md`](docs/superpowers/specs/2026-06-29-tuna-design.md) for the design (lives in the project's design-doc workspace).

## Requirements for analyzed services

The target Deployment must:

1. **Expose `/metrics`** with `prometheus/client_golang`'s Go collector:
   ```go
   import "github.com/prometheus/client_golang/prometheus/promhttp"
   http.Handle("/metrics", promhttp.Handler())
   ```
2. **Be scraped by Prometheus**, e.g. via a `ServiceMonitor` if you use kube-prometheus-stack.

That's it. tuna does the rest.

## Status

`v1alpha1` — expect breaking changes. The API surface (CRD shape) follows kubebuilder versioning conventions: additive changes within a version, breaking changes bump the version with a conversion webhook.

Roadmap:
- [x] Go workload analyzer (Phase 1)
- [ ] Node.js workload analyzer
- [ ] JVM workload analyzer
- [ ] AWS `sigv4` auth for Amazon Managed Prometheus
- [ ] Helm chart
- [ ] Apply mode (vertical autoscaling) — optional, opt-in

## License

[Apache 2.0](LICENSE)

## Contributing

PRs welcome. Two conventions to know up-front:

1. **Never push directly to `main`.** Use feature branches (`feat/...`, `fix/...`) and open a PR.
2. **Run lint + tests locally before pushing.** Same versions as CI:

```bash
make lint test           # required before push
make e2e-up e2e e2e-down # full e2e, optional but recommended for controller/analyzer changes
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for branch conventions, commit message style, release process, and repository layout.
