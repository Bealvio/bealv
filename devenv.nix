let
  sources = import ./npins;
in
{
  pkgs,
  lib,
  ...
}:
let
  # (re)generate gitops/apps/<app>/upstream from a nix/<name>.nix derivation
  buildUpstream = app: nixFile: ''
    set -e
    rm -rf gitops/apps/${app}/upstream
    mkdir -p gitops/apps/${app}/upstream
    cp -r --no-preserve=mode $(nix-build ${nixFile} "$@")/* gitops/apps/${app}/upstream/
  '';
in
{
  imports = [ "${sources.nixbook}/devenvModules/devenv.nix" ];

  # nix/*.nix build against the nixpkgs pinned in npins
  env.NIX_PATH = "nixpkgs=${sources.nixpkgs}";

  packages = with pkgs; [
    kustomize
    npins
    yq-go
  ];

  scripts = {
    buildDragonFly.description = "Build dragonfly-operator upstream manifests";
    buildDragonFly.exec = buildUpstream "dragonfly" "nix/dragonfly.nix";
    buildCnpg.description = "Build cloudnative-pg upstream manifests (requires version arg)";
    buildCnpg.exec = ''
      set -e
      rm -rf gitops/apps/cnpg/upstream
      mkdir -p gitops/apps/cnpg/upstream
      strippedVersion=$(echo "$1" | sed 's/^v//')
      cnpghash=$(nix-prefetch-url https://github.com/cloudnative-pg/cloudnative-pg/releases/download/$1/cnpg-$strippedVersion.yaml)
      cp -r --no-preserve=mode $(nix-build nix/cnpg.nix --argstr manifest01Hash "$cnpghash" --argstr version $1)/* gitops/apps/cnpg/upstream/
    '';
    buildIngressContour.description = "Build ingress-contour upstream manifests";
    buildIngressContour.exec = buildUpstream "ingress-controller-external" "nix/ingress-contour.nix";
    buildGatewayAPI.description = "Build gateway-api upstream manifests";
    buildGatewayAPI.exec = buildUpstream "gateway-api" "nix/gateway-api.nix";
  };

  enterShell = ''
    echo ""
    echo "bealv development environment loaded"
    echo ""
    echo "Available build scripts:"
    echo ""
    echo "  buildDragonFly             - Build dragonfly-operator upstream manifests"
    echo "  buildCnpg <version>        - Build cloudnative-pg upstream manifests"
    echo "  buildIngressContour        - Build ingress-contour upstream manifests"
    echo "  buildGatewayAPI            - Build gateway-api upstream manifests"
    echo ""
  '';
}
