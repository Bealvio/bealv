# AGENTS.md — Bealvio/bealv

Guide for AI agents (and humans) working in this repo. **Keep this file up to date:** whenever you make a significant change (new app, new exposure pattern, new upstream build script, change to update automation, new cross-repo dependency, or a gotcha you had to discover the hard way), update the relevant section here in the same PR/commit.

## What this repo is

GitOps source of truth for the **`bealv` workload cluster** ("Main Cluster"), reconciled by **FluxCD**. It only contains *workloads* (self-hosted apps and the app-level infra they need). The cluster itself, Flux, and most system add-ons are created and pushed from the management cluster repo **[Bealvio/flux-mgmt](https://github.com/Bealvio/flux-mgmt)**:

- Cluster API (Proxmox + Kamaji) creates the cluster; Sveltos ClusterProfiles install flux-operator/FluxInstance, Cilium, cert-manager, the internal contour ingress, external-snapshotter, proxmox-csi, trust-manager, monitoring CRDs/stack, external-secrets, etc. on it.
- Sveltos creates on this cluster: `GitRepository/infra` → this repo (`main`, only `/gitops`), `GitRepository/mgmt` → flux-mgmt (`/gitops`), and `Kustomization/apps` → `./gitops/kustomizations` (interval 2m, prune).
- ⇒ Changes to cert-manager, contour ingress, snapshotter, flux-operator or monitoring **upstream manifests happen in flux-mgmt**, not here, and also roll to this cluster.

## Layout

```
gitops/
  kustomizations/<app>.yaml   # one Flux Kustomization per app -> ./gitops/apps/<app> (sourceRef GitRepository/infra, interval 1m)
  apps/<app>/                 # plain manifests + kustomization.yaml (namespace set there), HelmReleases, ExternalSecrets…
  apps/*/upstream/            # GENERATED upstream bundles — don't hand-edit (see below)
  docs/superpowers/plans/     # implementation plans written by agents
nix/                          # derivations fetching upstream manifests (cnpg, dragonfly, gateway-api, contour, kube-prometheus)
npins/                        # pinned sources (nixpkgs, …)
updatecli/                    # update automation (updatecli.d/*.yaml + values.yaml)
shell.nix                     # nix-shell with kustomize + build scripts
```

### Adding a workload

1. Create `gitops/apps/<name>/` with a `kustomization.yaml` (set `namespace:`), a `namespace.yaml`, and the manifests.
2. Add `gitops/kustomizations/<name>.yaml` (copy an existing one, e.g. `trek.yaml`). Use `dependsOn` when needed (e.g. `kgateway` depends on `gateway-api`). Some stateful apps use `prune: false` (e.g. `zitadel`) — keep it that way.
3. Secrets: `ExternalSecret` → `ClusterSecretStore/vault-backend` (Vault paths under `secrets-bealv/…`). Never commit plaintext secrets.
4. Validate: `kustomize build gitops/apps/<name>`.

### Exposing services

- **Public (`*.bealv.io`)** — preferred, current pattern: an `HTTPRoute` with `parentRefs: [{name: https, namespace: kgateway-system}]` (kgateway `Gateway/https`, TLS via cert-manager issuer `bealvio`, DNS via external-dns/Cloudflare). There is also a contour `external` Gateway/ingress class fronted by `cloudflared` in `apps/ingress-controller-external`.
- **Legacy Ingress** (see README): internal `*.bealv.lan` with issuer `bealv`, or external with `ingressClassName: external` + issuer `bealvio`. Add label `probe: enabled` for blackbox monitoring.

## Environment & tooling

- `nix-shell` (`shell.nix`) provides `kustomize` and: `buildDragonFly`, `buildCnpg <version>`, `buildIngressContour` (→ `apps/ingress-controller-external/upstream`), `buildGatewayAPI`. (flux-mgmt uses devenv; this repo still uses `shell.nix` — propose migrating to `devenv.nix` rather than installing tools globally.)
- One-off tools: `nix run nixpkgs#fluxcd -- …`, `nix shell nixpkgs#yq-go -c yq …`.

## Update automation (bots)

- **updatecli** (`.github/workflows/updatecli-ci.yaml`, every 2h) with configs in `updatecli/updatecli.d/`:
  - `kubernetes-discovery.yaml`: container image bumps in `gitops/apps/` (ignores monitoring/contour upstream dirs) → PRs `deps: bump container image "<img>" to <ver>`.
  - `flux-discovery.yaml`: HelmRelease chart bumps → `deps: update Helm chart to <ver>` (title doesn't name the chart — check the diff).
  - Dedicated manifests: zitadel, grafana, immich (image tag in HelmRelease values), kgateway (both `helmrelease-crds.yaml` and `helmrelease.yaml` together), cnpg, dragonfly, gateway-api, ingress-contour.
- PR author: `update-chan` GitHub App. No CI checks run on PRs; review is manual.
- GitHub only allows **rebase merges** (`gh pr merge --rebase`).

## Reviewing / merging dependency PRs

1. Check the actual diff (usually a one-line tag change) and read upstream release notes for minor/major bumps — look for config key/env var renames, DB migrations (zitadel, immich, komga, vaultwarden run schema migrations on start — non-reversible), and CRD changes for charts (crossplane, kgateway).
2. `-development` tags (e.g. linuxserver bazarr) are intentionally tracked.
3. After merge Flux applies within ~1–2 min (GitRepository 1m, app Kustomizations 1m). Force with `flux reconcile source git infra -n flux-system` then `flux reconcile kustomization <app> -n flux-system`. API server: `10.250.0.13:6443` (kube context `bealv/kubernetes-admin@bealv-4c2fn`), only reachable from the LAN/VPN.

## Conventions

- Commit messages: `chore: …`, `feat ✨: …`, `fix: …`, `refactor 🎨 (scope): …`.
- Keep app manifests plain + kustomize; HelmReleases for charted apps (with a `helmrepo.yaml` next to them).
- Don't hand-edit `upstream/` dirs; patch them with kustomize patches alongside (e.g. `ingress-controller-external/*-patch.yaml`).

## Keep this file current

If you change anything described above, or learn something a future agent would otherwise have to rediscover, update this AGENTS.md in the same change. Also keep the sibling doc in Bealvio/flux-mgmt consistent when cross-repo behaviour changes.
