# `config/platfrom-mesh-ocm` — ManagedProvider via OCM

Deploys the kbind provider (operator + Angular portal) onto a PlatformMesh runtime
cluster from a **single, self-contained OCM component**,
`github.com/platform-mesh/kbind-provider`, published by this repo.

```sh
kubectl apply -k config/platfrom-mesh-ocm
```

## Self-contained component

The OCM component bundles everything the provider needs:

| Resource | Type | Origin |
|---|---|---|
| `operator-chart` | helmChart (local) | `kbind-provider-operator` chart, packaged + pushed by `make helm-push` |
| `portal-chart`   | helmChart (local) | `kbind-provider-portal` chart, packaged + pushed by `make helm-push` |
| `operator-image` | ociImage (local)  | `ghcr.io/platform-mesh/kbind-provider-operator` |
| `portal-image`   | ociImage (local)  | `ghcr.io/platform-mesh/kbind-provider-portal` |

(The kcp bootstrap manifests under `config/` are embedded in the init image via `go:embed`,
so they are not shipped as a separate component resource.)

Both charts are packaged and pushed by `make helm-push`, which must run **before** `ocm-build`
so OCM can resolve them (the release target `make ocm-release` chains
`images-push → helm-push → ocm-push`). `ocm-push` transfers with `--copy-resources`,
relocating all artifacts into `ghcr.io/platform-mesh`.

## How the ManagedProvider works

Both runtime deployments use an `ocm:` source pointing at the one component. You give the
OCM coordinates inline (`registry` + `component` + `version` + `resourceName`) and the
platform-mesh-operator creates the `delivery.ocm.software` `Repository`/`Component`/
`Resource` objects; the ocm-controller resolves the descriptor and each chart is deployed
via Flux (`OCIRepository` + `HelmRelease`).

```yaml
ocm:
  name: kbind-provider-operator                          # generated object names
  registry: ghcr.io/platform-mesh                        # → Repository (created by operator)
  component: github.com/platform-mesh/kbind-provider # → Component  (created by operator)
  version: "0.0.1"
  resourceName: operator-chart                           # resource within the component
  values: {...}                                          # Helm values (how to configure)
```

`name` is set explicitly on each entry because operator and portal resolve from the **same**
component and would otherwise collide on the generated object names.

The operator lifecycle (WaitPlatformMesh → ProviderResource → WaitProvider →
KubeconfigCopy → Deploy) provisions a dedicated kcp provider workspace and copies a scoped
admin kubeconfig into `platform-mesh-system` as Secret `kbind-provider-kubeconfig`. The
operator mounts this kubeconfig to connect to the kcp provider workspace.

## Prerequisites

1. The ocm-controller (ocm-k8s-toolkit) must be installed in the runtime cluster
   (provides the `delivery.ocm.software` CRDs).
2. The component must be published first:
   ```sh
   make ocm-push            # build + transfer (relocating all resources) to ghcr.io/platform-mesh
   ```
   Keep the `version:` in each `ocm:` entry in sync with the published `VERSION`.
3. Set the front-proxy ClusterIP in [kustomization.yaml](kustomization.yaml) so the backend
   pod can resolve `root.kcp.localhost` (see the comment there).
