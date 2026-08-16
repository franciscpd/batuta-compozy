#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

GUARD=scripts/check-compozy-version.sh

expect_reject() {
  local version=$1 commit=$2 out
  if out=$("$GUARD" --version "$version" --commit "$commit" 2>&1); then
    printf 'expected %s to be rejected\n' "$version" >&2
    return 1
  fi
  case "$out" in
    *"incompatible CompozyOS"*"v0.3.0-beta.14"*) ;;
    *)
      printf 'reject message must name the floor: %s\n' "$out" >&2
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
  if ! grep -q 'custom post-tag build' "$err"; then
    printf 'post-tag build must warn on stderr: %s\n' "$(cat "$err")" >&2
    rm -f "$err"
    return 1
  fi
  rm -f "$err"
}

expect_reject "v0.3.0-beta.13" "tag"
expect_reject "v0.3.0-beta.13-14-g36bd8156" "36bd8156"
expect_reject "v0.3.0-beta.13-5-g594d9fdf" "594d9fdf"
expect_reject "v0.2.9" "x"
expect_reject "v0.2.9-beta.99" "x"
expect_reject "garbage" "x"
expect_reject "" "x"

expect_accept "v0.3.0-beta.14" "x"
expect_accept "0.3.0-beta.14" "x"
expect_accept "v0.3.0-beta.16" "x"
expect_accept "v0.3.0" "x"
expect_accept "v0.3.1-beta.1" "x"
expect_accept "v0.4.0-beta.1" "x"
expect_accept "v1.0.0" "x"

expect_accept_with_warning "v0.3.0-beta.14-1-gabcdef12" "abcdef12"
expect_accept_with_warning "v0.3.0-beta.16-9-ga35eda6d" "a35eda6d"
expect_accept_with_warning "v0.3.0-beta.16-9-ga35eda6d" "a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c"
expect_accept_with_warning "v0.3.0-9-gdeadbeef" "deadbeef"

if "$GUARD" --version "v0.3.0-beta.14" >/dev/null 2>&1; then
  printf 'guard accepted a malformed argument list\n' >&2
  exit 1
fi

repo_had_compozy=false
if [[ -e .compozy || -L .compozy ]]; then
  repo_had_compozy=true
fi
"$GUARD" >/dev/null 2>&1
if [[ $repo_had_compozy == false && ( -e .compozy || -L .compozy ) ]]; then
  printf 'version guard generated .compozy in the repository\n' >&2
  exit 1
fi
printf 'OK: runtime guard enforces the v0.3.0-beta.14 semver floor\n'
