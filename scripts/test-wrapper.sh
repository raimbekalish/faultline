#!/bin/sh
# Unit-test Docker selection without creating contexts or contacting a daemon.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/faultline-wrapper-test.XXXXXXXX")
trap 'rm -rf "$test_dir"' EXIT
mkdir "$test_dir/bin"
cat > "$test_dir/bin/docker" <<'STUB'
#!/bin/sh
if [ "$1" = context ] && [ "$2" = inspect ]; then
  echo "$FAULTLINE_TEST_ENDPOINT"
  exit 0
fi
echo reached >> "$FAULTLINE_TEST_LOG"
exit 87
STUB
chmod +x "$test_dir/bin/docker"
check() {
  name=$1 expected=$2 host=$3 context=$4 endpoint=$5
  rm -f "$test_dir/calls"
  set +e
  PATH="$test_dir/bin:$PATH" DOCKER_HOST="$host" DOCKER_CONTEXT="$context" \
    FAULTLINE_TEST_ENDPOINT="$endpoint" FAULTLINE_TEST_LOG="$test_dir/calls" \
    sh "$root/scripts/experiment.sh" > "$test_dir/output" 2>&1
  code=$?
  set -e
  if [ "$code" -ne "$expected" ]; then cat "$test_dir/output" >&2; echo "$name: expected $expected, got $code" >&2; exit 1; fi
  if [ "$expected" = 2 ] && [ -e "$test_dir/calls" ]; then echo "$name: reached Compose for remote endpoint" >&2; exit 1; fi
  if [ "$expected" = 1 ] && [ ! -e "$test_dir/calls" ]; then echo "$name: local context incorrectly refused" >&2; exit 1; fi
  echo "PASS: $name"
}
check 'remote host refused' 2 tcp://unrelated.invalid:2375 '' ''
check 'remote context overrides local host' 2 unix:///tmp/local.sock remote ssh://unrelated.invalid
check 'local context overrides remote host' 1 tcp://unrelated.invalid:2375 local unix:///tmp/local.sock
check 'remote default context refused' 2 '' '' ssh://unrelated.invalid
