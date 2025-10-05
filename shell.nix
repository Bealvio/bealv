{
  pkgs ? import <nixpkgs> { },
}:
pkgs.mkShell {
  buildInputs = [
    pkgs.kustomize
  ];
  packages = [
    (pkgs.writeShellScriptBin "buildDragonFly" ''
      #!/bin/bash
      set -e
      rm -rf gitops/apps/dragonfly/upstream
      mkdir -p gitops/apps/dragonfly/upstream
      cp -r --no-preserve=mode $(nix-build nix/dragonfly.nix)/* gitops/apps/dragonfly/upstream/
    '')
    (pkgs.writeShellScriptBin "buildCnpg" ''
      #!/bin/bash
      set -e
      rm -rf gitops/apps/cnpg/upstream
      mkdir -p gitops/apps/cnpg/upstream
      strippedVersion=$(echo "$1" | sed 's/^v//')
      cnpghash=$(nix-prefetch-url https://github.com/cloudnative-pg/cloudnative-pg/releases/download/$1/cnpg-$strippedVersion.yaml)
      cp -r --no-preserve=mode $(nix-build nix/cnpg.nix --argstr manifest01Hash "$cnpghash" --argstr version $1)/* gitops/apps/cnpg/upstream/
    '')
    (pkgs.writeShellScriptBin "buildIngressContour" ''
      #!/bin/bash
      set -e
      rm -rf gitops/apps/ingress-controller-external/upstream
      mkdir -p gitops/apps/ingress-controller-external/upstream
      cp -r --no-preserve=mode $(nix-build nix/ingress-contour.nix)/* gitops/apps/ingress-controller-external/upstream/
    '')
  ];
}
