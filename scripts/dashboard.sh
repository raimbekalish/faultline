#!/bin/sh
# One native loopback origin. This command never starts fault or collector services.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
go_cmd=${FAULTLINE_GO:-go}
if ! command -v "$go_cmd" >/dev/null 2>&1; then
  echo 'Dashboard needs Go 1.26+ and a C compiler; install Go or set FAULTLINE_GO to its path.' >&2
  exit 2
fi
npm --prefix "$root/web" ci --ignore-scripts
npm --prefix "$root/web" run build
mkdir -p "$root/artifacts/bin"
CGO_ENABLED=1 "$go_cmd" build -trimpath -o "$root/artifacts/bin/dashboard" "$root/cmd/dashboard"
cd "$root"
exec "$root/artifacts/bin/dashboard" "$@"
