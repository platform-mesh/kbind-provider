# PlatformMesh Demo

End-to-end demo: deploy the kbind provider, stand up a consumer cluster, and bind a provider's APIs to it.

For pure code iteration without rebuilding images, see [DEVELOPMENT.md](DEVELOPMENT.md).
For the full image build and Helm reference, see [DEPLOYMENT.md](DEPLOYMENT.md).

## 1. Get the PlatformMesh Kubeconfig

```bash
cp ../helm-charts/.secret/kcp/admin.kubeconfig kcp-admin.kubeconfig
export PM_KUBECONFIG="$(realpath kcp-admin.kubeconfig)"
kind export kubeconfig --name platform-mesh --kubeconfig compute.kubeconfig
export COMPUTE_KUBECONFIG="$(realpath compute.kubeconfig)"
```

## 2. Deploy the kbind Provider

Follow [DEPLOYMENT.md](DEPLOYMENT.md) to build and deploy the operator and portal into the
platform-mesh cluster. Once done, confirm the pods are healthy:

```bash
kubectl get pods -n kbind-system \
  -l 'app.kubernetes.io/name in (kbind-provider-operator,kbind-provider-portal)'
```

## 3. Create a Consumer Kind Cluster

The platform-mesh kind cluster is the **provider** (runs kcp + the kbind operator + portal).
To exercise the full bind flow we need a second cluster — the **consumer** — where the kbind
konnector will run and where bound APIs become available.

```bash
kind create cluster --name kbind-consumer
kind export kubeconfig --name kbind-consumer --kubeconfig consumer.kubeconfig
export CONSUMER_KUBECONFIG="$(realpath consumer.kubeconfig)"
```

Confirm it is up:

```bash
KUBECONFIG=$CONSUMER_KUBECONFIG kubectl get nodes
```

> **TODO:** Add instructions for deploying the kbind konnector (`ghcr.io/kbind-dev/konnector`)
> via its Helm chart onto the consumer cluster.

### Networking note: resolving `root.kcp.localhost` from the consumer

The kubeconfigs handed out by the provider's portal point at `https://root.kcp.localhost:8443`.
Both kind clusters share the default `kind` docker network, and the platform-mesh kind cluster
publishes 8443 on host loopback. The consumer's konnector reaches kcp by:

1. Resolving `root.kcp.localhost` inside the konnector pod to the host-gateway IP (the IP
   `host.docker.internal` resolves to from inside the kind node). This must be set via
   `hostAliases` on the konnector deployment, because the name is not in any real DNS.
2. The kind node forwards that to the host's published `127.0.0.1:8443`.
3. The platform-mesh Istio gateway accepts the TLS handshake, routes by SNI `root.kcp.localhost`
   (`kcp-root-shard-tlsroute` in the `infra` chart), and lands on the root shard, which serves
   the workspace path embedded in the kubeconfig.

No extra kind-network configuration is needed beyond keeping both clusters on the default `kind`
docker network.

## 4. Deploy the Wildwest Provider

With the consumer cluster running, deploy the
[`platform-mesh/provider-quickstart`](https://github.com/platform-mesh/provider-quickstart)
wildwest provider — a ready-made example provider that exports a `Cowboys` API:

```bash
kubectl apply -k https://github.com/platform-mesh/provider-quickstart/config/platfrom-mesh-ocm
```

This applies the wildwest `ManagedProvider` to the platform-mesh cluster. The
platform-mesh-operator drives the full lifecycle:

1. Creates a kcp `Provider` (dedicated workspace + scoped admin kubeconfig).
2. Copies the kubeconfig into `platform-mesh-system` as `wildwest-provider-kubeconfig`.
3. Installs the wildwest-controller and wildwest-portal Helm charts via Flux.
   The controller's init container bootstraps the `Cowboys` APIExport, APIResourceSchema,
   and ProviderMetadata into the provider workspace before the controller starts.

Monitor progress:

```bash
kubectl get managedprovider wildwest -n platform-mesh-system -w
kubectl get pods -n platform-mesh-system \
  -l 'app.kubernetes.io/name in (wildwest-controller,wildwest-portal)'
```

## 5. Connect the Consumer Cluster

### Bind the providers

In the platform-mesh portal, bind both the `wildwest.platform-mesh.io` and
`kbind-provider.platform-mesh.io` APIExports to the consumer's kcp workspace. This makes the
Cowboys API and the kbind connection machinery available in that workspace.

### Generate a kbind bundle

Open the **kbind portal** and click **+ Add connection**:

1. Enter a **Bundle name** (e.g. `bobs-wildwest`).
2. Choose the API scope:
   - **All APIs** — the bundle covers every API exported by the provider.
   - **Select specific APIs** — pick individual APIs from the list.
3. Click **Generate bundle**. The portal assembles a YAML bundle containing:
   - a `Secret` with the provider kubeconfig,
   - a `Connection` referencing that Secret,
   - a `ClusterBinding` (only when specific APIs are selected).
4. Click **Copy** to copy the bundle YAML to the clipboard.
5. Click **Save connection** to close the dialog and record the `ConnectedCluster` on the
   provider side.

### Apply the bundle to the consumer cluster

Paste the copied YAML and apply it:

```bash
kubectl apply -f - --kubeconfig $CONSUMER_KUBECONFIG
# (paste bundle, then Ctrl-D)
```

### Verify the connection

On the **consumer cluster**, confirm the `Connection` is ready:

```bash
KUBECONFIG=$CONSUMER_KUBECONFIG kubectl get connections.core.kbind.io
NAME            READY   SECRET          AGE
bobs-wildwest   True    bobs-wildwest   10s
```

On the **kcp provider side** (inside the user's workspace), confirm the `ConnectedCluster`
is heartbeating:

```bash
kubectl get connectedclusters.kbind-provider.platform-mesh.io
NAME            CONNECTED   READY   LAST HEARTBEAT   AGE
bobs-wildwest   True        True    20s              30s
```

In the **kbind portal**, the connection should now appear in **Established** state.
