#!/bin/sh
# Own one uniquely named Compose project. Never tear down any other project.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
# Refuse remote Docker endpoints; experiments are local-only in this milestone.
docker_endpoint=${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}')}
case "$docker_endpoint" in
  unix://*) ;;
  *) echo "Faultline requires a local Docker Unix socket; remote endpoints are refused." >&2; exit 2 ;;
esac
run_dir=$(mktemp -d "${TMPDIR:-/tmp}/faultline.XXXXXXXX")
export FAULTLINE_PROJECT=$(basename "$run_dir" | tr '[:upper:].' '[:lower:]-')
export FAULTLINE_REVISION=$(git -C "$root" rev-parse --short HEAD)
if [ -n "$(git -C "$root" status --porcelain)" ]; then
  FAULTLINE_REVISION="${FAULTLINE_REVISION}-dirty"
fi
compose() { docker compose -p "$FAULTLINE_PROJECT" -f "$root/deploy/compose.yaml" "$@"; }
cleanup() {
  code=$?
  trap - EXIT INT TERM
  if ! compose down --volumes --remove-orphans --timeout 5 >&2; then
    echo "Cleanup failed. Recover with: FAULTLINE_PROJECT=$FAULTLINE_PROJECT docker compose -p $FAULTLINE_PROJECT -f $root/deploy/compose.yaml down --volumes" >&2
    code=1
  else
    # Remove only this run's image tags; leave shared base images/build cache.
    for image in "faultline-sample:$FAULTLINE_PROJECT" "faultline-test:$FAULTLINE_PROJECT"; do
      if docker image inspect "$image" >/dev/null 2>&1; then
        if ! docker image rm "$image" >&2; then code=1; fi
      fi
    done
  fi
  rm -rf "$run_dir"
  exit "$code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mode=run
if [ "${1:-}" = "test" ]; then mode=test; shift; fi
if [ "$mode" = test ] && [ "$#" -ne 0 ]; then echo "test accepts no arguments" >&2; exit 2; fi
compose build runner >&2
compose up -d --wait --wait-timeout 30 frontend >&2
if [ "$mode" = test ]; then
  compose build test >&2
  compose run --rm -T --no-deps test
else
  compose run --rm -T --no-deps runner run "$@"
fi
