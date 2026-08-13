#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

TMP=$(mktemp -d /tmp/batuta-package-lock-test.XXXXXX)
PACKAGE_ROOT="$TMP/packages"
GLOBAL_LOCK_ROOT="$TMP/global-lock"
READY_FIFO="$TMP/ready"
RELEASE_FIFO="$TMP/release"
PID=""
cleanup() {
  if [[ -n $PID ]] && kill -0 "$PID" 2>/dev/null; then
    printf 'release\n' > "$RELEASE_FIFO" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  case "$TMP" in
    /tmp/batuta-package-lock-test.*)
      chmod -R u+w "$TMP"
      rm -rf -- "$TMP"
      ;;
  esac
}
trap cleanup EXIT

mkfifo "$READY_FIFO" "$RELEASE_FIFO"
cat > "$TMP/compozy" <<'SH'
#!/usr/bin/env bash
case "$*" in
  "version -o json")
    printf '%s\n' '{"Version":"v0.3.0-beta.13-15-g4154d25c","Commit":"4154d25c89794dff634fccef00a3d968fc09c3f9","BuildDate":"test"}'
    ;;
  "extension validate "*)
    printf 'ready\n' > "$BATUTA_READY_FIFO"
    IFS= read -r _ < "$BATUTA_RELEASE_FIFO"
    printf '%s\n' '{"issues":[]}'
    ;;
  "extension list -o json") printf '%s\n' '[]' ;;
  "extension install "*) printf '%s\n' '{}' ;;
  "extension enable batuta -o json")
    printf '%s\n' '{"extension":{"state":"active"}}'
    ;;
  "extension inventory batuta -o json")
    printf '%s\n' '{"items":[{"kind":"agent","name":"batuta","live":true},{"kind":"loop","name":"batuta-deliver","live":true},{"kind":"skill","name":"batuta-routing","live":true}]}'
    ;;
  *) exit 99 ;;
esac
SH
chmod +x "$TMP/compozy"

BATUTA_PACKAGE_ROOT="$PACKAGE_ROOT" \
  BATUTA_REPUBLISH_LOCK_ROOT="$GLOBAL_LOCK_ROOT" BATUTA_READY_FIFO="$READY_FIFO" \
  BATUTA_RELEASE_FIFO="$RELEASE_FIFO" PATH="$TMP:$PATH" \
  scripts/republish.sh > "$TMP/out" 2> "$TMP/err" &
PID=$!

IFS= read -r ready < "$READY_FIFO"
[[ $ready == ready ]]

if ! python3 - "$GLOBAL_LOCK_ROOT/batuta-republish.lock" <<'PY'
import fcntl
import os
import sys

fd = os.open(sys.argv[1], os.O_CREAT | os.O_RDWR, 0o600)
try:
    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
except BlockingIOError:
    raise SystemExit(0)
raise SystemExit(1)
PY
then
  printf 'republish did not hold the global Batuta publication lock\n' >&2
  exit 1
fi

printf 'release\n' > "$RELEASE_FIFO"
wait "$PID"
PID=""
printf 'OK: global lock spans package creation and extension consumption\n'
