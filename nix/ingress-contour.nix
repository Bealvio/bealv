{
  pkgs ? import <nixpkgs> { },
}:
let
  # inherit (pkgs) lib;
  sources = import ../npins;
  manifest01 = sources.contour;
in
pkgs.runCommand "ingress-contour"
  {
    nativeBuildInputs = [
      pkgs.kustomize
    ];
  }
  ''
    set -e
    mkdir -p $out/
    kustomize init
    cp ${manifest01}/examples/contour/*.yaml ./
    cp ${manifest01}/examples/gateway/00-crds.yaml ./
    kustomize edit add resource *.yaml
    cp ${manifest01}/examples/contour/*.yaml $out/
    cp ${manifest01}/examples/gateway/00-crds.yaml $out/
    cp kustomization.yaml $out/
  ''
