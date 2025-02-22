# Main Cluster

This repo only contains working workload.

Most of the system and infra workload is deployed through automation.

## How to add workload

Create a kustomization fluxcd file that point to `gitops/apps/<workloadname>` and put it in `gitops/kustomizations`.

You workload will be deployed.
