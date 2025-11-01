{
  pkgs ? import <nixpkgs> { },
}:
let
  sources = import ../npins;
  manifest01 = sources.gateway-api;
in
pkgs.runCommand "gateway-api"
  {
    nativeBuildInputs = [
      pkgs.kustomize
    ];
  }
  ''
    set -e
    mkdir -p $out/
    kustomize init
    kustomize build ${manifest01}/config/crd/experimental > ./experimental.yaml
    kustomize edit add resource *.yaml
    cp experimental.yaml $out/
    cp kustomization.yaml $out/
  ''
