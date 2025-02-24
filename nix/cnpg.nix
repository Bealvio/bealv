{
  pkgs ? import <nixpkgs> { },
  version ? "",
  manifest01Hash ? "",
}:
let
  strippedVersion = builtins.replaceStrings [ "v" ] [ "" ] version;
  # inherit (pkgs) lib;
  manifest01 = pkgs.fetchurl {
    url = "https://github.com/cloudnative-pg/cloudnative-pg/releases/download/${version}/cnpg-${strippedVersion}.yaml";
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
    cp ${manifest01} ./cnpg-${strippedVersion}.yaml
    kustomize edit add resource *.yaml
    cp ${manifest01} $out/cnpg-${strippedVersion}.yaml
    cp kustomization.yaml $out/
  ''
