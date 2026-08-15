#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

GUARD=scripts/check-compozy-version.sh
FIX_COMMIT=594d9fdf041e722fca5f60a62351c4c084c71430

expect_reject() {
  local version=$1 commit=$2 out
  if out=$("$GUARD" --version "$version" --commit "$commit" 2>&1); then
    printf 'expected %s to be rejected\n' "$version" >&2
    return 1
  fi
  case "$out" in
    *"known official descendant"*"594d9fdf"*"custom-history counts"*) ;;
    *)
      printf 'recovery message missing compatibility boundary: %s\n' "$out" >&2
      return 1
      ;;
  esac
}

expect_accept() {
  local version=$1 commit=$2
  "$GUARD" --version "$version" --commit "$commit" >/dev/null
}

expect_reject "v0.3.0-beta.13" "594d9fdf"
expect_reject "v0.3.0-beta.13-5-g594d9fdf" "594d9fdf"
expect_reject "v0.3.0-beta.13-6-gdeadbee" "deadbeef"
expect_reject "v0.3.0-beta.13-6-g594d9fdf" "714b7347"
expect_reject \
  "v0.3.0-beta.13-6-g594d9fdf11111111111111111111111111111111" \
  "594d9fdf22222222222222222222222222222222"
expect_accept "v0.3.0-beta.13-6-g594d9fdf" "594d9fdf"
expect_accept "v0.3.0-beta.13-6-g594d9fdf" "$FIX_COMMIT"
expect_accept "v0.3.0-beta.13-14-g36bd8156" "36bd8156"
expect_accept \
  "v0.3.0-beta.13-15-g4154d25c" \
  "4154d25c89794dff634fccef00a3d968fc09c3f9"
expect_accept \
  "v0.3.0-beta.15-6-gef1bc78d" \
  "ef1bc78d0f6c1cd02adadd949483439bb0f43b6c"
expect_accept "v0.3.0-beta.15-6-gef1bc78d" "ef1bc78d"
expect_reject "v0.3.0-beta.15-6-gef1bc78d" "ef1bc7"
CURRENT_COMPOZY_COMMIT=c88b3e5274e86103215fbf900faf742d6593b7dd

expect_accept \
  "v0.3.0-beta.15-26-gc88b3e52" \
  "$CURRENT_COMPOZY_COMMIT"
expect_accept "v0.3.0-beta.15-26-gc88b3e52" "c88b3e52"
expect_reject "v0.3.0-beta.15-26-gc88b3e52" "c88b3e5"
expect_reject \
  "v0.3.0-beta.15-25-gc88b3e52" \
  "$CURRENT_COMPOZY_COMMIT"
expect_reject \
  "v0.3.0-beta.15-26-gc88b3e52" \
  "c88b3e5274e86103215fbf900faf742d6593b7de"
CURRENT_SPEC_CYCLE_COMMIT=a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c

expect_accept \
  "v0.3.0-beta.16-9-ga35eda6d" \
  "$CURRENT_SPEC_CYCLE_COMMIT"
expect_accept "v0.3.0-beta.16-9-ga35eda6d" "a35eda6d"
expect_reject "v0.3.0-beta.16-9-ga35eda6d" "a35eda6"
expect_reject \
  "v0.3.0-beta.16-8-ga35eda6d" \
  "$CURRENT_SPEC_CYCLE_COMMIT"
expect_reject \
  "v0.3.0-beta.16-9-ga35eda6d" \
  "a35eda6d3a2ec47995c19a14a5a01d4f9452cf1d"
expect_accept "v0.3.0-beta.14" "deadbeef"
expect_accept "v0.3.0" "deadbeef"

official_out=$("$GUARD" \
  --version "v0.3.0-beta.13-15-g4154d25c" \
  --commit "4154d25c89794dff634fccef00a3d968fc09c3f9")
case "$official_out" in
  *"known official descendant"*"594d9fdf"*) ;;
  *)
    printf 'official-build qualification missing: %s\n' "$official_out" >&2
    exit 1
    ;;
esac

repo_had_compozy=false
if [[ -e .compozy || -L .compozy ]]; then
  repo_had_compozy=true
fi
"$GUARD" >/dev/null
if [[ $repo_had_compozy == false && ( -e .compozy || -L .compozy ) ]]; then
  printf 'version guard generated .compozy in the repository\n' >&2
  exit 1
fi
printf 'OK: runtime compativel e fronteira post-beta.13 coberta\n'
