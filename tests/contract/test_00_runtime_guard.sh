#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

GUARD=scripts/check-compozy-version.sh

expect_reject() {
  local version=$1 out
  if out=$("$GUARD" --version "$version" 2>&1); then
    printf 'expected %s to be rejected\n' "$version" >&2
    return 1
  fi
  case "$out" in
    *"official linear git-describe"*"594d9fdf"*"custom histories"*) ;;
    *)
      printf 'recovery message missing compatibility boundary: %s\n' "$out" >&2
      return 1
      ;;
  esac
}

expect_accept() {
  local version=$1
  "$GUARD" --version "$version" >/dev/null
}

expect_reject "v0.3.0-beta.13"
expect_reject "v0.3.0-beta.13-5-gdeadbee"
expect_accept "v0.3.0-beta.13-6-g594d9fdf"
expect_accept "v0.3.0-beta.13-14-g36bd8156"
expect_accept "v0.3.0-beta.14"
expect_accept "v0.3.0"

official_out=$("$GUARD" --version "v0.3.0-beta.13-14-g36bd8156")
case "$official_out" in
  *"official linear git-describe provenance"*"custom-history counts"*) ;;
  *)
    printf 'official-build qualification missing: %s\n' "$official_out" >&2
    exit 1
    ;;
esac

"$GUARD" >/dev/null
printf 'OK: runtime compativel e fronteira post-beta.13 coberta\n'
