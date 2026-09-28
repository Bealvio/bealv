#!/usr/bin/env bash
# Renovate postUpgradeTask (see renovate.json5), run inside `devenv shell`.
# Renovate has already bumped `version` of <pin> in npins/sources.json;
# refresh the rest of the pin and regenerate the bundle built from it.
#
# Usage: scripts/renovate-post-upgrade.sh <pin> <version>
set -euo pipefail

pin="$1"
version="${2:-}"

npins update --partial "$pin"

case "$pin" in
cloudnative-pg) buildCnpg "$version" ;;
contour) buildIngressContour ;;
dragonfly-operator) buildDragonFly ;;
gateway-api) buildGatewayAPI ;;
nixbook) ;; # devenv module, nothing to generate
*)
  echo "renovate-post-upgrade: no build step known for pin '$pin'" >&2
  exit 1
  ;;
esac

treefmt
