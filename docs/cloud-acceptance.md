# Cloud Acceptance Procedure (GCP / GMP)

This procedure verifies tuna end-to-end against Google Managed Prometheus (GMP) on a real GKE cluster. Run **once per release milestone** before tagging.

Phase 1 covers GCP only. AWS/AMP is Phase 2.

## Prerequisites

- `gcloud` CLI authenticated to a project with billing enabled
- `kubectl`, `helm` installed locally
- `make` + `docker`

Cost: ~$0.50 for a complete run (~30 minutes wall time).

## Procedure

### 1. Provision cluster with managed Prometheus

```bash
export PROJECT=$(gcloud config get-value project)
export REGION=us-central1
export CLUSTER=tuna-acceptance

gcloud container clusters create $CLUSTER --region $REGION --num-nodes=1 --machine-type=e2-medium
gcloud container clusters update $CLUSTER --region $REGION --enable-managed-prometheus
gcloud container clusters get-credentials $CLUSTER --region $REGION
```

### 2. Configure Workload Identity for the operator

```bash
gcloud iam service-accounts create tuna-sa
gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:tuna-sa@$PROJECT.iam.gserviceaccount.com" \
  --role="roles/monitoring.viewer"

# Bind KSA (will be created when we apply install.yaml) → GSA
gcloud iam service-accounts add-iam-policy-binding \
  tuna-sa@$PROJECT.iam.gserviceaccount.com \
  --role="roles/iam.workloadIdentityUser" \
  --member="serviceAccount:$PROJECT.svc.id.goog[tuna-system/tuna-manager]"
```

### 3. Deploy tuna

```bash
kubectl apply -f https://github.com/siabroo/tuna/releases/latest/download/install.yaml

# Annotate the ServiceAccount so Workload Identity activates
kubectl annotate serviceaccount tuna-manager -n tuna-system \
  iam.gke.io/gcp-service-account=tuna-sa@$PROJECT.iam.gserviceaccount.com

# Point at GMP + enable auth mode
kubectl set env deployment/tuna-manager -n tuna-system \
  PROMETHEUS_URL=https://monitoring.googleapis.com/v1/projects/$PROJECT/location/global/prometheus \
  AUTH_MODE=gcp-id-token

kubectl rollout restart deployment/tuna-manager -n tuna-system
kubectl rollout status deployment/tuna-manager -n tuna-system --timeout=2m
```

### 4. Deploy sample workload

```bash
# Build + push sample app to a registry GKE can pull
cd test/samples/sample-go-app
docker build -t gcr.io/$PROJECT/sample-go-app:dev .
docker push gcr.io/$PROJECT/sample-go-app:dev
cd ../../..

# Edit deployment.yaml to use the gcr.io image, then:
kubectl apply -f test/samples/sample-go-app/deployment.yaml

# For GMP, scrape via PodMonitoring CRD instead of ServiceMonitor:
cat <<EOF | kubectl apply -f -
apiVersion: monitoring.googleapis.com/v1
kind: PodMonitoring
metadata:
  name: sample-go-app
  namespace: default
spec:
  selector:
    matchLabels:
      app: sample-go-app
  endpoints:
    - port: http
      interval: 30s
      path: /metrics
EOF
```

### 5. Wait + verify

```bash
# Wait ~10 min for samples to accumulate
sleep 600

# Check the CR
kubectl describe workloadrecommendation sample-go-app
```

Expected output:
```
Status:
  Conditions:
    Last Transition Time: ...
    Reason:               AnalysisFresh
    Status:               True
    Type:                 Ready
    Reason:               PrometheusReachable
    Status:               True
    Type:                 MetricsAvailable
    Reason:               Detected
    Status:               True
    Type:                 WorkloadDetected
    Reason:               SamplesAdequate
    Status:               True
    Type:                 DataSufficient
  Recommendations:
    Current:      100m
    Field:        resources.requests.cpu
    Rationale:    p95 CPU usage ...m exceeds requests 100m; scheduler will mispack the pod.
    Recommended: ...
    Severity:     warning
  ...
```

### 6. Capture output

Save the `kubectl describe` output to a dated file:

```bash
kubectl describe workloadrecommendation sample-go-app > \
  docs/cloud-acceptance/$(date +%Y-%m-%d)-gcp.md

git add docs/cloud-acceptance/
git commit -m "docs: GCP acceptance run results $(date +%Y-%m-%d)"
git push
```

This commit becomes evidence in the README that the cloud path works.

### 7. Cleanup

```bash
gcloud container clusters delete $CLUSTER --region $REGION --quiet
```

## Troubleshooting

**`MetricsAvailable=False` reason `PrometheusUnreachable`:**
- Verify Workload Identity binding (`gcloud iam service-accounts describe tuna-sa@...`)
- Check the operator pod's logs: `kubectl logs -n tuna-system deployment/tuna-manager`
- Verify the GMP API is enabled: `gcloud services list | grep monitoring.googleapis.com`

**`WorkloadDetected=False` reason `NoAnalyzerMatched`:**
- The sample-go-app may not be exporting `go_info` yet — give it 2–3 more minutes
- Verify scrape config: `kubectl get podmonitoring -A` and check that GMP is collecting

**Empty recommendations despite Ready=True:**
- All values within 10% suppression threshold — sample app may need more CPU stress
- Hit `/work` endpoint to generate CPU load: `kubectl exec -n default deployment/sample-go-app -- /metrics`
