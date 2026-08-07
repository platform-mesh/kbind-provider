# `config/platfrom-mesh-flux` — ManagedProvider via Flux

Deploys the kbind provider (operator + Angular portal) onto a PlatformMesh runtime
cluster directly from **OCI Helm chart artifacts**, published by this repo.

```sh
kubectl apply -k config/platfrom-mesh-flux
```

## How it works

Both runtime deployments use a `flux:` source. The platform-mesh-operator creates a Flux
`OCIRepository` + `HelmRelease` for each entry, pointing at the published chart:

```yaml
flux:
  chart: kbind-provider-operator
  registry: ghcr.io/platform-mesh/kbind-provider/charts
  version: "0.0.1"
  values: {...}
```

The operator lifecycle (WaitPlatformMesh → ProviderResource → WaitProvider →
KubeconfigCopy → Deploy) provisions a dedicated kcp provider workspace and copies a scoped
admin kubeconfig into `platform-mesh-system` as Secret `kbind-provider-kubeconfig`. The
operator chart mounts this kubeconfig via its init container, which bootstraps the kcp
provider workspace (APIExport, APIResourceSchema, RBAC) before the main container starts.

## Prerequisites

1. **FluxCD** source-controller + helm-controller installed in the runtime cluster.
2. **platform-mesh-operator** installed and a `PlatformMesh` named `platform-mesh` is `Ready`.
3. Charts published to the OCI registry:
   ```sh
   make helm-push VERSION=0.0.1
   ```
   Keep the `version:` in each `flux:` entry in sync with the published `CHART_VERSION`.
4. Set the front-proxy ClusterIP in [`kustomization.yaml`](kustomization.yaml):
   ```sh
   kubectl -n platform-mesh-system get svc frontproxy-front-proxy \
     -o jsonpath='{.spec.clusterIP}'
   ```

## Observe

```bash
kubectl get managedprovider kbind -n platform-mesh-system -w

kubectl get secret kbind-provider-kubeconfig -n platform-mesh-system

kubectl get ocirepository,helmrelease -n platform-mesh-system

kubectl get pods -n platform-mesh-system \
  -l 'app.kubernetes.io/name in (kbind-provider-operator,kbind-provider-portal)'
```

## Tear down

```bash
kubectl delete -k config/platfrom-mesh-flux
```

Set `spec.cleanupOnDelete: true` in [`managedprovider.yaml`](managedprovider.yaml) to also
remove the kcp provider workspace on deletion.
