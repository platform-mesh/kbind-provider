# Local Deployment with Images

Build the provider container images, load them into a local kind cluster, and install via Helm. Use this mode to validate the full deployment path end-to-end.

For iterating on code without rebuilding images, see [DEVELOPMENT.md](DEVELOPMENT.md).

Before running Helm install, the kcp provider workspace kubeconfig secret must exist in the target namespace (see Prerequisites below).

## Prerequisites

The operator connects to the kcp provider workspace via a kubeconfig stored in a Kubernetes Secret. Create it before installing the chart:

```bash
NAMESPACE=kbind-system

kubectl create namespace $NAMESPACE
kubectl delete secret kbind-provider-kubeconfig -n $NAMESPACE --ignore-not-found
kubectl create secret generic kbind-provider-kubeconfig \
  --from-file=kubeconfig=provider.kubeconfig \
  -n $NAMESPACE
```

`provider.kubeconfig` must point at the kcp provider workspace. See [DEVELOPMENT.md](DEVELOPMENT.md) for how to obtain it.

## Local Registry (Kind) — one-line deployment

If your Kind cluster is configured with a local registry (see the
[Kind local registry guide](https://kind.sigs.k8s.io/docs/user/local-registry/)), you can
build, push images and charts to it, then deploy everything with a single `kubectl apply -k`
via the [Flux ManagedProvider](../config/platfrom-mesh-flux/README.md):

```bash
# Point the build at your local registry (e.g. localhost:5001)
export IMAGE_REGISTRY=localhost:5001
export VERSION=0.0.1

# Build and push images + Helm charts to the local registry
make images-push helm-push IMAGE_REGISTRY=$IMAGE_REGISTRY VERSION=$VERSION

# Deploy operator + portal (ManagedProvider drives the full lifecycle)
kubectl apply -k config/platfrom-mesh-flux
```

Update `registry` and `version` in [`config/platfrom-mesh-flux/managedprovider.yaml`](../config/platfrom-mesh-flux/managedprovider.yaml)
to match your local registry and version before applying. Also set the front-proxy ClusterIP
in [`config/platfrom-mesh-flux/kustomization.yaml`](../config/platfrom-mesh-flux/kustomization.yaml)
(see the [Flux ManagedProvider README](../config/platfrom-mesh-flux/README.md) for details).

## Container Images

### Build and Load into Kind (typical workflow)

```bash
export IMAGE_TAG=platform-mesh
make images kind-load-all IMAGE_TAG=$IMAGE_TAG
```

### Individual Targets

```bash
# Build images
make operator-image-build     # operator (includes init subcommand)
make portal-image-build       # portal UI
make images                   # both

# Load into kind
make kind-load-operator
make kind-load-portal
make kind-load-all

# Push to registry
make images-push
make operator-image-push
make portal-image-push
```

### Override Variables

```bash
make images IMAGE_TAG=v0.1.0
make images IMAGE_REGISTRY=my-registry.io/org
make kind-load-all KIND_CLUSTER=my-cluster
```

### Run Portal Container Locally

```bash
make portal-run                # foreground (http://localhost:4300)
make portal-run-detached       # background
make portal-stop               # stop background container
```

## Helm Deployment

### Deploy Operator

The operator chart includes an init container that bootstraps the kcp provider workspace (APIExport, APIResourceSchema, RBAC) on every pod start, so no separate bootstrap step is needed.

```bash
helm upgrade --install kbind-provider-operator \
  deploy/helm/kbind-provider-operator \
  -n $NAMESPACE --create-namespace \
  --set image.tag=$IMAGE_TAG
```

### Deploy Portal

```bash
# Update chart dependencies
make helm-deps

# Install portal (assumes IMAGE_TAG was exported above).
# Enable httpRoute + middleware so the portal is reachable via the platform-mesh gateway.
helm upgrade --install kbind-provider-portal \
  deploy/helm/kbind-provider-portal \
  -n $NAMESPACE \
  --set image.tag=$IMAGE_TAG \
  --set httpRoute.enabled=true \
  --set middleware.enabled=true
```
