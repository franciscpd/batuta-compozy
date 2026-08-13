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
expect_accept "v0.3.0-beta.14" "deadbeef"
expect_accept "v0.3.0" "deadbeef"

official_out=$("$GUARD" --version "v0.3.0-beta.13-14-g36bd8156" --commit "36bd8156")
case "$official_out" in
  *"known official descendant"*"594d9fdf"*) ;;
  *)
    printf 'official-build qualification missing: %s\n' "$official_out" >&2
    exit 1
    ;;
esac

repo_had_compozy=false
if [[ -e .compozy ]]; then
  repo_had_compozy=true
fi
"$GUARD" >/dev/null
if [[ $repo_had_compozy == false && -e .compozy ]]; then
  printf 'version guard generated .compozy in the repository\n' >&2
  exit 1
fi
printf 'OK: runtime compativel e fronteira post-beta.13 coberta\n'
