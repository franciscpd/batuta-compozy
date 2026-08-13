#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

TMP=$(mktemp -d /tmp/batuta-global-lock-test.XXXXXX)
GLOBAL_LOCK_ROOT="$TMP/global-lock"
READY_FIFO="$TMP/ready"
RELEASE_FIFO="$TMP/release"
ATTEMPT_FIFO="$TMP/attempt"
LOG="$TMP/calls"
FIRST_PID=""
SECOND_PID=""
cleanup() {
  if [[ -n $FIRST_PID ]] && kill -0 "$FIRST_PID" 2>/dev/null; then
    printf 'release\n' > "$RELEASE_FIFO" 2>/dev/null || true
  fi
  for pid in "$FIRST_PID" "$SECOND_PID"; do
    if [[ -n $pid ]] && kill -0 "$pid" 2>/dev/null; then
      wait "$pid" 2>/dev/null || true
    fi
  done
  case "$TMP" in
    /tmp/batuta-global-lock-test.*)
      chmod -R u+w "$TMP"
      rm -rf -- "$TMP"
      ;;
  esac
}
trap cleanup EXIT

mkfifo "$READY_FIFO" "$RELEASE_FIFO" "$ATTEMPT_FIFO"
cat > "$TMP/compozy" <<'SH'
#!/usr/bin/env bash
printf '%s:%s\n' "$BATUTA_PUBLISHER_ID" "$*" >> "$BATUTA_FAKE_LOG"
case "$*" in
  "version -o json")
    printf '%s\n' '{"Version":"v0.3.0-beta.13-15-g4154d25c","Commit":"4154d25c89794dff634fccef00a3d968fc09c3f9","BuildDate":"test"}'
    ;;
  "extension validate "*)
    if [[ $BATUTA_PUBLISHER_ID == first ]]; then
      printf 'ready\n' > "$BATUTA_READY_FIFO"
      IFS= read -r _ < "$BATUTA_RELEASE_FIFO"
    fi
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

BATUTA_PUBLISHER_ID=first BATUTA_FAKE_LOG="$LOG" \
  BATUTA_PACKAGE_ROOT="$TMP/packages-a" \
  BATUTA_REPUBLISH_LOCK_ROOT="$GLOBAL_LOCK_ROOT" \
  BATUTA_READY_FIFO="$READY_FIFO" BATUTA_RELEASE_FIFO="$RELEASE_FIFO" \
  PATH="$TMP:$PATH" scripts/republish.sh > "$TMP/first.out" 2> "$TMP/first.err" &
FIRST_PID=$!
IFS= read -r ready < "$READY_FIFO"
[[ $ready == ready ]]

if [[ ! -f $GLOBAL_LOCK_ROOT/batuta-republish.lock ]]; then
  printf 'republish did not acquire the stable global Batuta lock\n' >&2
  exit 1
fi

exec 9<> "$ATTEMPT_FIFO"
BATUTA_PUBLISHER_ID=second BATUTA_FAKE_LOG="$LOG" \
  BATUTA_PACKAGE_ROOT="$TMP/packages-b" \
  BATUTA_REPUBLISH_LOCK_ROOT="$GLOBAL_LOCK_ROOT" \
  BATUTA_REPUBLISH_LOCK_ATTEMPT_FD=9 PATH="$TMP:$PATH" \
  scripts/republish.sh > "$TMP/second.out" 2> "$TMP/second.err" &
SECOND_PID=$!
IFS= read -r attempt <&9
[[ $attempt == attempt ]]

while IFS= read -r call; do
  if [[ $call == second:* ]]; then
    printf 'second publisher entered the global transaction before release: %s\n' \
      "$call" >&2
    exit 1
  fi
done < "$LOG"

printf 'release\n' > "$RELEASE_FIFO"
wait "$FIRST_PID"
FIRST_PID=""
wait "$SECOND_PID"
SECOND_PID=""
exec 9>&-

python3 - "$LOG" <<'PY'
import sys

calls = open(sys.argv[1]).read().splitlines()
first_end = calls.index("first:extension inventory batuta -o json")
second_start = calls.index("second:version -o json")
assert first_end < second_start, calls
print("OK: distinct package roots serialize one global Batuta transaction")
PY
