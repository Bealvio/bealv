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
npins/                        # pinned upstream sources (format v8, npins >= 0.5) driving the generated upstream/ dirs
scripts/renovate-post-upgrade.sh  # pin -> build script mapping, run by Renovate after a pin bump
renovate.json5                # Renovate config (all dependency updates)
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

## Update automation (Renovate)

All dependency updates come from **self-hosted Renovate**:
- **Workflow:** `.github/workflows/renovate.yaml` runs every 4h, on manual dispatch, and on pushes that change the config.
- **Identity:** the `update-chan` GitHub App (secrets `UPCLI_APP_ID`/`UPCLI_SECRET_ID`).
- **Config:** `renovate.json5`.
- **Overview:** the **Dependency Dashboard** issue lists every pending or approval-gated update. Tick a checkbox there to force one.

What is covered:

| Source | Manager |
|---|---|
| Container images in manifests under `gitops/` | `kubernetes` |
| HelmRelease charts (HelmRepository / OCI) and images in HelmRelease `spec.values` | `flux` |
| `npins/sources.json` release pins: cloudnative-pg, contour, dragonfly-operator, gateway-api | custom regex → `postUpgradeTasks` |
| CloudNativePG `imageName` (postgres majors disabled) | custom regex |
| Values with a `# renovate: datasource=<ds> depName=<name>` comment on the line above: Grafana CR version, immich server tag, the Renovate version itself | custom regex |
| pz-control Go module (weekly, `go mod tidy`) and its Dockerfile | `gomod` / `dockerfile` |
| Workflow actions (weekly, grouped) | `github-actions` |

**npins flow:** Renovate bumps `version`, then runs `nix-shell --run './scripts/renovate-post-upgrade.sh <pin> <version>'`. The script runs `npins update --partial <pin>` and the matching `build*` generator from `shell.nix`, so the PR contains the whole regenerated `upstream/` bundle (images, CRDs, RBAC). When adding a pin or generator, add its case to that script.

**Generated `upstream/` dirs** (cnpg, dragonfly, gateway-api, ingress-controller-external) are in `ignorePaths`. `crossplane/upstream` is hand-written and *is* managed.

**Deliberately excluded:**
- the unused `kube-prometheus`/`grafana-operator`/`kubevirt-monitoring` pins, since monitoring comes from flux-mgmt;
- VectorChord (`immich/cnpg.yaml`), because immich dictates the supported version;
- floating tags (`latest`, `stable`, `main`).

**Grouping:** kgateway CRDs + controller charts share one PR, as do the ARC charts. Identical charts (oauth2-proxy ×7, nfs-provisioner ×2) share a PR automatically.

**To make Renovate track a new value it can't detect,** add a `# renovate:` comment above it.

Other details:
- Commits and PR titles look like `chore(deps): update <dep> to <version>`. Updates must be at least 2 days old (except `zot.bealv.io/*`). No automerge; no CI checks, so review is manual.
- GitHub only allows **rebase merges** (`gh pr merge --rebase`).
- ⚠️ Never let a chart `version:` become a `sha256-…` string. updatecli's digest mode once pinned the immich chart to a cosign signature tag, which is not a chart, and broke the HelmRelease.

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
