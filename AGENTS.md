# AGENTS.md — Bealvio/bealv

Guide for AI agents (and humans) working in this repo. **Keep this file up to date:** whenever you make a significant change (new app, new exposure pattern, new upstream build script, change to update automation, new cross-repo dependency, or a gotcha you had to discover the hard way), update the relevant section here in the same PR/commit.

## What this repo is

GitOps source of truth for the **`bealv` workload cluster** ("Main Cluster"), reconciled by **FluxCD**. It only contains _workloads_ (self-hosted apps and the app-level infra they need). The cluster itself, Flux, and most system add-ons are created and pushed from the management cluster repo **[Bealvio/flux-mgmt](https://github.com/Bealvio/flux-mgmt)**:

- Cluster API (Proxmox + Kamaji) creates the cluster; Sveltos ClusterProfiles install flux-operator/FluxInstance, Cilium, cert-manager, the internal contour ingress, external-snapshotter, proxmox-csi, trust-manager, monitoring CRDs/stack, external-secrets, etc. on it.
- Sveltos creates on this cluster: `GitRepository/infra` → this repo (`main`, only `/gitops`), `GitRepository/mgmt` → flux-mgmt (`/gitops`), and `Kustomization/apps` → `./gitops/kustomizations` (interval 10m, prune).
- ⇒ Changes to cert-manager, contour ingress, snapshotter, flux-operator or monitoring **upstream manifests happen in flux-mgmt**, not here, and also roll to this cluster.

## Layout

```
gitops/
  kustomizations/<app>.yaml   # one Flux Kustomization per app -> ./gitops/apps/<app> (sourceRef GitRepository/infra, interval 10m; 30m for CRD/upstream bundles)
  apps/<app>/                 # plain manifests + kustomization.yaml (namespace set there), HelmReleases, ExternalSecrets…
  apps/*/upstream/            # GENERATED upstream bundles — don't hand-edit (see below)
  docs/superpowers/plans/     # implementation plans written by agents
nix/                          # derivations fetching upstream manifests (cnpg, dragonfly, gateway-api, contour, kube-prometheus)
npins/                        # pinned upstream sources (format v8, npins >= 0.5) driving the generated upstream/ dirs
scripts/renovate-post-upgrade.sh  # pin -> build script mapping, run by Renovate after a pin bump
renovate.json5                # Renovate config (all dependency updates)
devenv.nix / devenv.yaml     # dev shell (devenv) + build scripts; devenv.lock pins its inputs
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

- Use **devenv**: `devenv shell -- <cmd>`, or `direnv` (`.envrc` runs `use devenv`).
  - The shell provides `kustomize`, `npins`, `yq`, `treefmt` (from the nixbook devenv module, pinned in npins) and the generators `buildDragonFly`, `buildCnpg <version>`, `buildIngressContour` (→ `apps/ingress-controller-external/upstream`) and `buildGatewayAPI`.
  - `NIX_PATH` is set to the npins `nixpkgs` pin, so `nix/*.nix` builds are reproducible.
  - Entering the shell installs pre-commit hooks (treefmt = prettier/nixfmt/shfmt/gofumpt, shellcheck, mdsh), and treefmt may reformat files. Run `devenv shell treefmt` before committing.
- If `devenv shell` fails with ``The option `dotenv.resolved' was accessed but has no value defined``, the devenv CLI is newer than `devenv.lock`: run `devenv update` and commit the lock.
- One-off tools: `nix run nixpkgs#fluxcd -- …`, `nix shell nixpkgs#yq-go -c yq …`.

## Update automation (Renovate)

All dependency updates come from **self-hosted Renovate**:

- **Workflow:** `.github/workflows/renovate.yaml` runs every 4h, on manual dispatch, and on pushes that change the config.
- **Identity:** the `update-chan` GitHub App (secrets `UPCLI_APP_ID`/`UPCLI_SECRET_ID`).
- **Config:** `renovate.json5`.
- **Overview:** the **Dependency Dashboard** issue lists every pending or approval-gated update. Tick a checkbox there to force one.

What is covered:

| Source                                                                                                                                                   | Manager                           |
| -------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| Container images in manifests under `gitops/`                                                                                                            | `kubernetes`                      |
| HelmRelease charts (HelmRepository / OCI) and images in HelmRelease `spec.values`                                                                        | `flux`                            |
| `npins/sources.json` release pins: cloudnative-pg, contour, dragonfly-operator, gateway-api                                                              | custom regex → `postUpgradeTasks` |
| CloudNativePG `imageName` (postgres majors disabled)                                                                                                     | custom regex                      |
| Values with a `# renovate: datasource=<ds> depName=<name>` comment on the line above: Grafana CR version, immich server tag, the Renovate version itself | custom regex                      |
| pz-control Go module (weekly, `go mod tidy`) and its Dockerfile                                                                                          | `gomod` / `dockerfile`            |
| Workflow actions (weekly, grouped)                                                                                                                       | `github-actions`                  |

**npins flow:** Renovate bumps `version`, then runs `devenv shell -- ./scripts/renovate-post-upgrade.sh <pin> <version>`. The script runs `npins update --partial <pin>` the matching `build*` generator from `devenv.nix` and `treefmt`, so the PR contains the whole regenerated `upstream/` bundle (images, CRDs, RBAC). When adding a pin or generator, add its case to that script.

**Generated `upstream/` dirs** (cnpg, dragonfly, gateway-api, ingress-controller-external) are in `ignorePaths`. `crossplane/upstream` is hand-written and _is_ managed.

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

### Renovate gotchas

- **Missing app permissions:**
  - The `update-chan` app has no _Commit statuses_ permission. A 403 when Renovate sets a `renovate/*` status makes it abort the whole run as "Repository has changed during renovation", so `statusCheckNames` are disabled in `renovate.json5`. Re-enable them if the permission is granted.
  - Without _Dependabot alerts: read_, the dashboard shows "Cannot access vulnerability alerts" (harmless).
  - Without _Workflows: write_, Renovate cannot push GitHub Actions updates.
- **Branches left by a crashed run** show up on the dashboard as "PR Edited (Blocked)". If such a `renovate/*` branch has no PR, delete it and Renovate recreates it cleanly.
- **Don't push to `main` while a Renovate run is in progress** (Actions tab): the run aborts and picks up again on the next schedule.
- **Debug runs:** trigger the workflow with `logLevel=debug`. Long lines get truncated in the web log view; download the full log with `gh api repos/<owner>/<repo>/actions/runs/<id>/logs > logs.zip`.
- **devenv and git worktrees:** `devenv shell` inside a git worktree points the shared pre-commit hook at that worktree. After removing the worktree, commits fail with "config file not found": re-enter `devenv shell` in the main checkout.

## Reviewing / merging dependency PRs

1. Check the actual diff (usually a one-line tag change) and read upstream release notes for minor/major bumps — look for config key/env var renames, DB migrations (zitadel, immich, komga, vaultwarden run schema migrations on start — non-reversible), and CRD changes for charts (crossplane, kgateway).
2. `-development` tags (e.g. linuxserver bazarr) are intentionally tracked.
3. After merge Flux applies within ~1–2 min (GitRepository 1m; a new revision triggers the Kustomizations at once, their 10m/30m intervals only pace drift correction). Force with `flux reconcile source git infra -n flux-system` then `flux reconcile kustomization <app> -n flux-system`. API server: `10.250.0.13:6443` (kube context `bealv/kubernetes-admin@bealv-4c2fn`), only reachable from the LAN/VPN.

## Conventions

- Commit messages: `chore: …`, `feat ✨: …`, `fix: …`, `refactor 🎨 (scope): …`.
- Keep app manifests plain + kustomize; HelmReleases for charted apps (with a `helmrepo.yaml` next to them).
- Don't hand-edit `upstream/` dirs; patch them with kustomize patches alongside (e.g. `ingress-controller-external/*-patch.yaml`).
- CNPG operator resources: `apps/cnpg/controller-resources-patch.yaml` raises upstream's 100m CPU limit (it was throttled and slow to honour `nodeMaintenanceWindow` during node drains).

## Backups

`gitops/apps/pvc-backups` (Kustomization `pvc-backups`): Velero Schedules on this cluster, kopia file-level backups to bucket `velero-bealv` on minio-clusters (mgmt, ~98Gi), kept 7 days.

- `app-data` (02:00 UTC): app volumes of the listed namespaces. Add a namespace there when you add a stateful app. The hdda/hddb NFS media shares are skipped by the `velero-volume-policies` ConfigMap; a PVC with an empty StorageClass would NOT be skipped.
- `databases` (02:30 UTC): every CloudNativePG cluster. A pre-hook writes `pg_dumpall` to `/var/lib/postgresql/data/velero-pg_dumpall.sql` before the copy; restore from that dump with `psql -f`, not from the copied PGDATA. Add new CNPG namespaces here.
- Not backed up: immich photos (`immich-data`, ~430Gi, too big for the bucket), Nextcloud user files and media (NFS shares), zot, Prometheus.
- Restore: `velero restore create --from-backup <backup> --include-namespaces <ns>`.

## Suspended Kustomizations

Some Kustomizations get suspended by hand on the live cluster (`flux suspend`); that isn't in git. As of 2026-10-02: `arc`, `prowlarr`, `radarr`, `sonarr`, `spegel`. Resuming applies the current `main`, and with `prune: true` Flux deletes anything in the old inventory that the new build no longer has. Before `flux resume`, diff `kubectl -n flux-system get kustomization <name> -o jsonpath='{.status.inventory.entries[*].id}'` against the object IDs of `kustomize build <path>` (`<ns>_<name>_<group>_<kind>`); resume only if nothing would disappear.

## Keep this file current

If you change anything described above, or learn something a future agent would otherwise have to rediscover, update this AGENTS.md in the same change. Also keep the sibling doc in Bealvio/flux-mgmt consistent when cross-repo behaviour changes.
