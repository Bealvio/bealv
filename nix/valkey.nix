{
  pkgs ? import <nixpkgs> { },
  version ? "",
  manifest01Hash ? "",
}:
let
  strippedVersion = builtins.replaceStrings [ "v" ] [ "" ] version;
  # inherit (pkgs) lib;
  manifest01 = pkgs.fetchurl {
    url = "https://github.com/hyperspike/valkey-operator/releases/download/${version}/install.yaml";
    sha256 = "${manifest01Hash}";
  };
in
pkgs.runCommand "capi"
  {
    nativeBuildInputs = [
      pkgs.kustomize
    ];
  }
  ''
    set -e
    mkdir -p $out/
    kustomize init
    cp ${manifest01} ./install.yaml
    kustomize edit add resource *.yaml
    cp ${manifest01} $out/install.yaml
    cp kustomization.yaml $out/
  ''
