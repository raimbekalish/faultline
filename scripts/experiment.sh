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
if [ -n "${DOCKER_CONTEXT:-}" ]; then
  docker_endpoint=$(docker context inspect "$DOCKER_CONTEXT" --format '{{.Endpoints.docker.Host}}')
elif [ -n "${DOCKER_HOST:-}" ]; then
  docker_endpoint=$DOCKER_HOST
else
  docker_endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
fi
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
  if ! compose down --volumes --remove-orphans --timeout 12 >&2; then
    echo "Cleanup failed. Recover with: FAULTLINE_PROJECT=$FAULTLINE_PROJECT docker compose -p $FAULTLINE_PROJECT -f $root/deploy/compose.yaml -f $root/deploy/compose.tracing.yaml -f $root/deploy/compose.ui.yaml down --volumes --remove-orphans --timeout 12" >&2
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
case "${1:-}" in run|test|evidence|history) mode=$1; shift ;; esac
if [ "$mode" = test ] && [ "$#" -ne 0 ]; then echo "test accepts no arguments" >&2; exit 2; fi
if [ "$mode" = evidence ] && { [ "$tracing" != 1 ] || [ "$#" -ne 0 ]; }; then echo "evidence requires --tracing and accepts no arguments" >&2; exit 2; fi
if [ "$mode" = history ] && [ "$tracing" = 1 ]; then echo "history queries do not start a tracing environment" >&2; exit 2; fi

# Mount only the selected input file and database directory. Never mount the
# Docker socket. The database bind survives Compose teardown; the root stays RO.
spec_path=
db_path=
expect=
for arg do
  if [ "$expect" = spec ]; then spec_path=$arg; expect=; continue; fi
  if [ "$expect" = db ]; then db_path=$arg; expect=; continue; fi
  case "$arg" in
    -spec|--spec) expect=spec ;;
    -db|--db) expect=db ;;
    -spec=*|--spec=*) spec_path=${arg#*=} ;;
    -db=*|--db=*) db_path=${arg#*=} ;;
  esac
done
if [ -n "$expect" ]; then echo "missing specification/database flag value" >&2; exit 2; fi
persistent=0
if [ "$mode" = history ] || [ -n "$spec_path" ] || [ -n "$db_path" ]; then
  persistent=1
  if [ "$(id -u)" = 0 ]; then echo "persistent container commands require a non-root host user" >&2; exit 2; fi
  db_path=${db_path:-artifacts/faultline.sqlite}
  if [ "$mode" != history ]; then (umask 077; mkdir -p -- "$(dirname -- "$db_path")"); fi
  db_dir=$(CDPATH= cd -- "$(dirname -- "$db_path")" && pwd)
  db_name=$(basename -- "$db_path")
  if [ -n "$spec_path" ]; then
    test -f "$spec_path"
    spec_path=$(CDPATH= cd -- "$(dirname -- "$spec_path")" && pwd)/$(basename -- "$spec_path")
  fi
  # Rewrite path flags without eval or word splitting. The for-loop snapshots
  # the original argv; appended rewritten arguments are retained after shift.
  original_count=$#
  expect=
  for arg do
    if [ "$expect" = spec ]; then arg=/input/spec.json; expect=
    elif [ "$expect" = db ]; then arg=/state/$db_name; expect=
    else
      case "$arg" in
        -spec|--spec) expect=spec ;;
        -db|--db) expect=db ;;
        -spec=*|--spec=*) arg=-spec=/input/spec.json ;;
        -db=*|--db=*) arg=-db=/state/$db_name ;;
      esac
    fi
    set -- "$@" "$arg"
  done
  shift "$original_count"
  if [ "$mode" = history ]; then
    history_command=${1:-}
    if [ -z "$history_command" ]; then echo "history command required" >&2; exit 2; fi
    shift
    set -- "$history_command" -db "/state/$db_name" "$@"
  else
    set -- -db "/state/$db_name" "$@"
  fi
fi
compose build runner >&2
if [ "$mode" != history ]; then
  if [ "$tracing" = 1 ]; then compose up -d --wait --wait-timeout 30 jaeger >&2; fi
  compose up -d --wait --wait-timeout 30 frontend >&2
fi
if [ "$mode" = test ]; then
  compose build test >&2
  compose run --rm -T --no-deps test
else
  if [ "$persistent" = 1 ]; then
    if [ -n "$spec_path" ]; then
      compose run --rm -T --no-deps --user "$(id -u):$(id -g)" -v "$db_dir:/state" -v "$spec_path:/input/spec.json:ro" runner "$mode" "$@"
    else
      compose run --rm -T --no-deps --user "$(id -u):$(id -g)" -v "$db_dir:/state" runner "$mode" "$@"
    fi
  else
    compose run --rm -T --no-deps runner "$mode" "$@"
  fi
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
