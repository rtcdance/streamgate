# StreamGate Kubernetes Deployment

Kustomize-based K8s deployment with base/overlays pattern.

## Structure

```
deploy/k8s/
├── base/                     # Environment-agnostic manifests
│   ├── kustomization.yaml    # Base Kustomization
│   ├── namespace.yaml
│   ├── configmap.yaml
│   ├── secret.yaml           # ExternalSecret CRDs + local dev secrets
│   ├── pdb.yaml              # PodDisruptionBudgets
│   ├── rbac.yaml             # ServiceAccount, Roles, Bindings
│   ├── infrastructure/       # PostgreSQL, Redis, MinIO, Prometheus, Grafana
│   ├── services/             # 8 microservices (Deployment + Service + HPA)
│   │   └── hpa/              # HorizontalPodAutoscalers (split out)
│   └── monolith/             # Monolithic mode (all-in-one)
│
├── overlays/                 # Environment-specific overlays
│   ├── dev/                  # Development (replicas=1, relaxed resources)
│   ├── staging/              # Staging (medium scale, HPA enabled)
│   └── prod/                 # Production (HA replicas, TLS ingress, NetworkPolicy)
│
└── patches/                  # Shared patches (replicas, resources, HPA, PDB)
```

## Quick Start

```bash
# Dev (default)
make k8s-up

# One-click with acceptance tests
make k8s-acceptance

# View status
make k8s-status

# Stream logs
make k8s-logs

# Teardown
make k8s-down
```

## Deploy to Different Environments

```bash
# Dev (default, reducd overlay
K8S_OVERLAY=dev make k8s-up

# Staging
K8S_OVERLAY=staging make k8s-up

# Production
K8S_OVERLAY=prod make k8s-up
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `k8s-up` | Apply secrets + deploy overlay |
| `k8s-down` | Delete all overlay resources |
| `k8s-status` | Show deployments, pods, services, HPA |
| `k8s-logs` | Stream logs from all pods |
| `k8s-verify` | Render + dry-run validate manifests |
| `k8s-diff` | Show diff between current and desired state |
| `k8s-rollout` | Wait for rollout to complete |
| `k8s-oneclick` | `up` + `rollout` + `status` |
| `k8s-acceptance` | `up` + `rollout` + acceptance tests |

## Overlays Comparison

| Feature | dev | staging | prod |
|---------|-----|---------|------|
| Replicas | 1 per svc | 2-4 per svc | 3-10 per svc |
| Reqautoscaling/v2K PA | min:1, max:3 | min:2, max:10 | min:3, max:20 |
| HPA | Disabled | Enabled | Enabled |
| Ingress TLS | No | No | ✅ Let's Encrypt |
| NetworkPolicy | No | No | ✅ Zero-trust |
| PDB strictness | Low | Medium | High |
| Image tag | `dev` | `staging` | `latest` |
| Resources | Relaxed | Standard | Production |

## Prerequisites

- kubectl configured with cluster access
- kustomize (v4.x, or kubectl v1.14+ with built-in kustomize)
- Valid container registry access for images
- External Secrets Operator (prod only)
