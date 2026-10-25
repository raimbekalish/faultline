#!/bin/sh
# Own one uniquely named Compose project. Never tear down any other project.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tracing=0
view=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --tracing) tracing=1; shift ;;
    --view) view=1; shift ;;
    *) break ;;
  esac
done
if [ "$view" = 1 ] && [ "$tracing" != 1 ]; then echo "--view requires --tracing" >&2; exit 2; fi
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
compose() {
  if [ "$view" = 1 ]; then
    docker compose -p "$FAULTLINE_PROJECT" -f "$root/deploy/compose.yaml" -f "$root/deploy/compose.tracing.yaml" -f "$root/deploy/compose.ui.yaml" "$@"
  elif [ "$tracing" = 1 ]; then
    docker compose -p "$FAULTLINE_PROJECT" -f "$root/deploy/compose.yaml" -f "$root/deploy/compose.tracing.yaml" "$@"
  else
    docker compose -p "$FAULTLINE_PROJECT" -f "$root/deploy/compose.yaml" "$@"
  fi
}
cleanup() {
  code=$?
  trap - EXIT INT TERM
  if [ -n "${viewer_pid:-}" ]; then
    kill "$viewer_pid" 2>/dev/null || :
    wait "$viewer_pid" 2>/dev/null || :
  fi
  if ! compose down --volumes --remove-orphans --timeout 8 >&2; then
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
case "${1:-}" in test|evidence) mode=$1; shift ;; esac
if [ "$mode" = test ] && [ "$#" -ne 0 ]; then echo "test accepts no arguments" >&2; exit 2; fi
if [ "$mode" = evidence ] && { [ "$tracing" != 1 ] || [ "$#" -ne 0 ]; }; then echo "evidence requires --tracing and accepts no arguments" >&2; exit 2; fi
compose build runner >&2
if [ "$tracing" = 1 ]; then compose up -d --wait --wait-timeout 30 jaeger >&2; fi
compose up -d --wait --wait-timeout 30 frontend >&2
if [ "$mode" = test ]; then
  compose build test >&2
  compose run --rm -T --no-deps test
else
  compose run --rm -T --no-deps runner "$mode" "$@"
fi
if [ "$view" = 1 ]; then
  viewer_address=$(compose port jaeger 16686)
  case "$viewer_address" in
    127.0.0.1:[1-9]*) ;;
    *) echo "Loopback viewer port was not published; refusing an invalid UI URL." >&2; exit 1 ;;
  esac
  echo "Jaeger UI: http://$viewer_address — available for five minutes; Ctrl-C cleans up early." >&2
  sleep 300 &
  viewer_pid=$!
  wait "$viewer_pid"
fi
