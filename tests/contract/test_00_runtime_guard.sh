#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

GUARD=scripts/check-compozy-version.sh
SDK_MODULE=github.com/compozy/compozy/sdk/go
SDK_VERSION=v0.3.0-beta.21

expect_reject() {
  local version=$1 commit=$2 out
  if out=$("$GUARD" --version "$version" --commit "$commit" 2>&1); then
    printf 'expected %s to be rejected\n' "$version" >&2
    return 1
  fi
  case "$out" in
    *"incompatible CompozyOS"*"v0.3.0-beta.21"*) ;;
    *)
      printf 'reject message must name the floor: %s\n' "$out" >&2
      return 1
      ;;
  esac
  case "$out" in
    *"unrecognized version format"*)
      printf 'below-floor reject must not claim unrecognized format: %s\n' "$out" >&2
      return 1
      ;;
  esac
}

expect_reject_unparseable() {
  local version=$1 commit=$2 out
  if out=$("$GUARD" --version "$version" --commit "$commit" 2>&1); then
    printf 'expected %s to be rejected\n' "$version" >&2
    return 1
  fi
  case "$out" in
    *"unrecognized version format"*"v0.3.0-beta.21"*) ;;
    *)
      printf 'unparseable reject message must name format and floor: %s\n' "$out" >&2
      return 1
      ;;
  esac
}

expect_accept() {
  local version=$1 commit=$2 out err
  err=$(mktemp)
  if ! out=$("$GUARD" --version "$version" --commit "$commit" 2>"$err"); then
    printf 'expected %s to be accepted: %s\n' "$version" "$(cat "$err")" >&2
    rm -f "$err"
    return 1
  fi
  case "$out" in
    "OK: "*) ;;
    *)
      printf 'accept output must start with OK: %s\n' "$out" >&2
      rm -f "$err"
      return 1
      ;;
  esac
  if [[ -s $err ]]; then
    printf 'release build must not warn: %s\n' "$(cat "$err")" >&2
    rm -f "$err"
    return 1
  fi
  rm -f "$err"
}

expect_accept_with_warning() {
  local version=$1 commit=$2 out err
  err=$(mktemp)
  if ! out=$("$GUARD" --version "$version" --commit "$commit" 2>"$err"); then
    printf 'expected %s to be accepted with warning: %s\n' "$version" "$(cat "$err")" >&2
    rm -f "$err"
    return 1
  fi
  case "$out" in
    "OK: "*) ;;
    *)
      printf 'accept output must start with OK: %s\n' "$out" >&2
      rm -f "$err"
      return 1
      ;;
  esac
  if ! grep -q 'custom compatible build' "$err"; then
    printf 'custom build must warn on stderr: %s\n' "$(cat "$err")" >&2
    rm -f "$err"
    return 1
  fi
  rm -f "$err"
}

expect_reject "v0.3.0-beta.20" "tag"
expect_reject "v0.3.0-beta.20-14-g36bd8156" "36bd8156"
expect_reject "v0.3.0-beta.14" "tag"
expect_reject "v0.2.9" "x"
expect_reject "v0.2.9-beta.99" "x"

expect_reject_unparseable "garbage" "x"
expect_reject_unparseable "" "x"
expect_reject_unparseable "v0.3.0-rc.1" "x"
expect_reject_unparseable "v0.4.0-rc.1" "x"
expect_reject_unparseable "V0.3.0" "x"
expect_reject_unparseable "v0.3.0-beta.21+meta" "x"
expect_reject_unparseable "v0.3.0-beta.21.preview." "x"
expect_reject_unparseable "v0.3.0-beta.21.preview.not-a-hash" "x"

expect_accept "v0.3.0-beta.21" "x"
expect_accept "0.3.0-beta.21" "x"
expect_accept "v0.3.0-beta.22" "x"
expect_accept "v0.3.0" "x"
expect_accept "v0.3.1-beta.1" "x"
expect_accept "v0.4.0-beta.1" "x"
expect_accept "v1.0.0" "x"

expect_accept_with_warning "v0.3.0-beta.21-1-gabcdef12" "abcdef12"
expect_accept_with_warning "v0.3.0-beta.22-9-g382976d4b" "382976d4b"
expect_accept_with_warning "v0.3.0-beta.22-9-g382976d4b" "382976d4b43274630a4b67445812fd4a0216dbcc"
expect_accept_with_warning "v0.3.0-9-gdeadbeef" "deadbeef"
expect_accept_with_warning "0.3.0-beta.21.preview.b53a4e14a" "b53a4e14a"
expect_accept_with_warning "v0.3.0-beta.22.preview.abcdef12" "abcdef12deadbeef"

if "$GUARD" --version "v0.3.0-beta.21" >/dev/null 2>&1; then
  printf 'guard accepted a malformed argument list\n' >&2
  exit 1
fi

resolved_sdk_version=$(go list -m -f '{{.Version}}' "$SDK_MODULE")
if [[ $resolved_sdk_version != "$SDK_VERSION" ]]; then
  printf 'SDK version = %s, want exact %s\n' "$resolved_sdk_version" "$SDK_VERSION" >&2
  exit 1
fi
if go mod edit -json | python3 -c '
import json
import sys

module = sys.argv[1]
payload = json.load(sys.stdin)
raise SystemExit(0 if any(row.get("Old", {}).get("Path") == module for row in payload.get("Replace") or []) else 1)
' "$SDK_MODULE"
then
  printf 'SDK module must not use a replace directive: %s\n' "$SDK_MODULE" >&2
  exit 1
fi

repo_had_compozy=false
if [[ -e .compozy || -L .compozy ]]; then
  repo_had_compozy=true
fi
guard_err=$(mktemp)
if ! "$GUARD" >/dev/null 2>"$guard_err"; then
  printf 'real compozy binary rejected by guard:\n%s\n' "$(cat "$guard_err")" >&2
  rm -f "$guard_err"
  exit 1
fi
rm -f "$guard_err"
if [[ $repo_had_compozy == false && ( -e .compozy || -L .compozy ) ]]; then
  printf 'version guard generated .compozy in the repository\n' >&2
  exit 1
fi
printf 'OK: runtime guard and SDK pin enforce the v0.3.0-beta.21 boundary\n'
