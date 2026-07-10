# kbind provider

This repository implements the kbind provider for PlatformMesh.io, exposing APIs from a kcp workspace to downstream consumer clusters via the kbind protocol.

## Architecture

**Provider side** — two images from this repo:

| Image | Description |
|-------|-------------|
| `ghcr.io/platform-mesh/kbind-provider-operator` | Operator that manages kbind APIExports and ConnectedCluster objects in the kcp provider workspace |
| `ghcr.io/platform-mesh/kbind-provider-portal` | Angular portal UI (Luigi microfrontend) for managing connections |

**Consumer side** — the user is expected to have the upstream kbind konnector deployed:

| Image | Description |
|-------|-------------|
| `ghcr.io/kbind-dev/konnector` | Upstream kbind konnector; syncs bound APIs to the consumer cluster |

## Guides

- **[Development](docs/DEVELOPMENT.md)** — run the operator (`go run`) and portal (`npm start`) directly from source against a local kcp + kind setup. Use for code iteration.
- **[Local Deployment with Images](docs/DEPLOYMENT.md)** — build container images, load them into kind, and install via Helm. Use to validate the full deployment path.
- **[PlatformMesh Demo](docs/PM-DEMO.md)** — end-to-end demo: get PM kubeconfig → bootstrap → load images → Helm install. Mixes the two above into a single happy-path script.

## Portal Features

- List all `ConnectedCluster` objects with connection status (Connected, Stale, Pending) and last heartbeat time
- Create a new `ConnectedCluster`: choose APIs (all or specific), generate the kbind bundle YAML, apply it to the consumer cluster, then save
- Edit an existing `ConnectedCluster`'s API selection and regenerate the bundle
- Delete a `ConnectedCluster` with confirmation
