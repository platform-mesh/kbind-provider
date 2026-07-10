# Development

Run the operator and portal directly from source against a local kcp + kind setup. Use this mode for iterating on code without rebuilding container images.

For container-image-based deployment (build, kind-load, Helm install), see [DEPLOYMENT.md](DEPLOYMENT.md).

## Prerequisites

```bash
cp ../helm-charts/.secret/kcp/admin.kubeconfig kcp-admin.kubeconfig # Platform-mesh local-setup (helm-charts repo)
export PM_KUBECONFIG="$(realpath kcp-admin.kubeconfig)"
kind export kubeconfig --name platform-mesh --kubeconfig compute.kubeconfig
export COMPUTE_KUBECONFIG="$(realpath compute.kubeconfig)"
```

## 1. Create Provider Workspace

```bash
KUBECONFIG=$PM_KUBECONFIG kubectl ws :root:providers
KUBECONFIG=$PM_KUBECONFIG kubectl create workspace kbind --type=root:provider --enter --ignore-existing
```

## 2. Bootstrap Provider Resources

Seed the kbind APIExport, APIResourceSchema, and RBAC into the provider workspace:

```bash
go run ./cmd/operator init --kcp-kubeconfig $PM_KUBECONFIG
```

## 3. Run the Operator

```bash
go run ./cmd/operator \
  --kcp-kubeconfig $PM_KUBECONFIG \
  --endpoint-slice kbind-provider.platform-mesh.io
```

## 4. Run the Portal UI

```bash
cd portal
npm install
npm start
```

The dev server starts at `http://localhost:4300`. The portal integrates with the Platform Mesh Portal via the Luigi microfrontend framework.
