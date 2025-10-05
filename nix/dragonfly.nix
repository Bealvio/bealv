{
  pkgs ? import <nixpkgs> { },
}:
let
  # inherit (pkgs) lib;
  sources = import ../npins;
  manifest01 = sources.dragonfly-operator;
in
pkgs.runCommand "dragonfly-operator"
  {
    nativeBuildInputs = [
      pkgs.kustomize
    ];
  }
  ''
    set -e
    mkdir -p $out/
    kustomize init
    cp ${manifest01}/manifests/dragonfly-operator.yaml ./
    kustomize edit add resource *.yaml
    cp ${manifest01}/manifests/dragonfly-operator.yaml $out/
    cp kustomization.yaml $out/
  ''
