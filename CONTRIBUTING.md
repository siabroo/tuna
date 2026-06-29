# Contributing to tuna

Thanks for considering a contribution. This document covers the conventions for
working on this repo.

## TL;DR pre-PR checklist

```bash
make lint test        # both must pass before push
git checkout -b feat/<your-feature>     # never push to main directly
# ... work ...
git push origin feat/<your-feature>
gh pr create                            # opens PR against main
```

## Branch convention

**Never push directly to `main`.** All changes go through pull requests:

- Branch name: `feat/<short-name>`, `fix/<short-name>`, `docs/<short-name>`, `ci/<short-name>`, `refactor/<short-name>`.
- One change per branch. Don't bundle unrelated work.
- Open a PR against `main` early — draft PRs are encouraged.

Rationale: `main` is the source of truth for releases (the `release.yml`
workflow builds and publishes from tags on `main`). A force-push or a broken
commit on `main` impacts everyone. PR review catches issues before they land.

## Run lint locally before pushing

The CI lint job uses `golangci-lint` v2.12.2 pinned in
`.github/workflows/lint.yml`. The Makefile target uses the **same pinned
version**, so what passes locally passes in CI:

```bash
make lint
```

This runs `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --timeout=5m`. First invocation downloads the binary; subsequent runs are fast (cached in `~/go/pkg/mod/`).

If you bump the lint version, update **both** `Makefile` and
`.github/workflows/lint.yml` together — drift between them is a common cause of
"works on my machine, fails in CI."

## Run tests locally before pushing

```bash
make test
```

This:
1. Generates DeepCopy + CRD manifests (`make generate manifests`)
2. Downloads envtest binaries to `testbin/` (cached after first run)
3. Runs `go test -race ./...` with `KUBEBUILDER_ASSETS` pointing at envtest

Unit tests + envtest integration tests must pass. The full suite takes ~30s on
a modern laptop after the first run (envtest binary download is a one-time
~100MB).

For an end-to-end run against a real cluster (kind + kube-prometheus-stack +
sample-go-app + tuna operator), use:

```bash
make e2e-up    # ~10 min: kind cluster + helm install + 5min sample wait
make e2e       # Go test asserts CR appears with recommendations
make e2e-down  # cleanup
```

E2E is **not required** before every push — it's slow and the in-process
envtest covers most of the same logic. Run it before merging major changes or
when modifying the kind/install path.

## Commit message style

Use short, focused commits with conventional-commits-style prefixes:

- `feat(scope): ...` — new feature
- `fix(scope): ...` — bug fix
- `docs(scope): ...` — documentation only
- `ci(scope): ...` — CI / workflow / lint config
- `refactor(scope): ...` — code restructuring with no behavior change
- `test(scope): ...` — tests only
- `chore(scope): ...` — tooling, deps, lockfile

Scope examples: `controller`, `analyzer`, `prom`, `selector`, `sample-go-app`, `lint`.

Body should explain **why**, not what (the diff shows what).

## Pull request process

1. Open the PR as **Draft** if work is in progress.
2. CI runs lint + test on every push. Fix any failures before requesting review.
3. For changes touching `internal/controller/` or `internal/analyzer/`, run
   `make e2e-up e2e e2e-down` locally and note the result in the PR body.
4. Add the `e2e` label to a PR to trigger the e2e workflow in CI on demand
   (it's not run on every PR by default — see `.github/workflows/e2e.yml`).
5. Squash-merge for small changes; merge commit for multi-commit features.

## Release process

Releases are tag-driven. Pushing a tag matching `v*` triggers
`.github/workflows/release.yml`:

1. Build + push image to `ghcr.io/siabroo/tuna:vX.Y.Z` (and `:latest`)
2. `kustomize build config/default` → `dist/install.yaml`
3. Create GitHub Release with auto-generated notes + attach `install.yaml`

```bash
git tag -a v0.1.0-alpha -m "Initial alpha release"
git push origin v0.1.0-alpha
```

## Style notes

- Go: standard `gofmt`. golangci-lint enforces the rest.
- YAML manifests: 2-space indent, no trailing whitespace.
- Markdown: 80-char soft wrap, ATX headers (`#`, not underlined).

## Repository layout

See [README.md](README.md#how-it-works) for an overview of which package does
what. Key directories:

- `api/v1alpha1/` — CRD Go types (DeepCopy generated, committed)
- `cmd/manager/` — operator binary entrypoint
- `internal/analyzer/` — recommendation math (pure logic, no k8s deps)
- `internal/controller/` — DiscoveryController + AnalysisController
- `internal/prom/` — PromQL HTTP client with pluggable auth
- `internal/selector/` — pod-to-deployment resolution
- `config/` — kustomize manifests (CRD, RBAC, manager, default)
- `test/samples/loadgen/` — test fixture (configurable CPU+memory load via HTTP API)
- `test/e2e/` — Go test guarded by `//go:build e2e`
- `dashboards/tuna.json` — Grafana dashboard for `kube-prometheus-stack`

## Questions

Open an issue. Tagging it with `question` helps.
