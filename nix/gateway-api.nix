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
    cp ${manifest01}/config/crd/standard/*.yaml ./
    kustomize edit add resource *.yaml
    cp ${manifest01}/config/crd/standard/*.yaml $out/
    cp kustomization.yaml $out/
  ''
