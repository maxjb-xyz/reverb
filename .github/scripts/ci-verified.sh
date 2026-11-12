#!/usr/bin/env bash
# Usage: ci-verified.sh <commit-sha>
#
# Prints "true" when CI has already passed for exactly this code, so a
# workflow that publishes it can skip running CI again; prints "false"
# otherwise. Needs GH_TOKEN and GITHUB_REPOSITORY.
#
# CI runs on pull requests only, not again on the push that merges them, so a
# merge commit usually has no CI run of its own. A pull_request run tests the
# PR head merged into its base branch as the base stood when the run started.
# The merge commit is that same code when either:
#   - it has the same tree as the PR head (the branch was up to date), or
#   - the base hadn't moved since the run started: the merge commit's first
#     parent (the base it was merged into) already existed when the run began.
# Otherwise the merge combined code that was never tested together, and the
# caller runs CI on it.
set -euo pipefail

sha="$1"
repo="$GITHUB_REPOSITORY"

# Start times of the passing CI runs on a commit, one per line.
green_runs() {
  gh run list --repo "$repo" --workflow ci.yml --commit "$1" \
    --status completed --json conclusion,createdAt \
    --jq '.[] | select(.conclusion == "success") | .createdAt'
}

commit() {
  gh api "repos/$repo/commits/$1" --jq "$2"
}

# A run on the commit itself (manual dispatch, or a run from before CI
# stopped running on pushes).
if [ -n "$(green_runs "$sha")" ]; then
  echo "CI passed on $sha itself." >&2
  echo true
  exit 0
fi

tree="$(commit "$sha" .commit.tree.sha)"
base="$(commit "$sha" '.parents[0].sha // ""')"
base_date=""
if [ -n "$base" ]; then
  base_date="$(date -u -d "$(commit "$base" .commit.committer.date)" +%s)"
fi

for head in $(gh api "repos/$repo/commits/$sha/pulls" \
  --jq '.[] | select(.merged_at != null) | .head.sha'); do
  runs="$(green_runs "$head")"
  [ -n "$runs" ] || continue
  if [ "$(commit "$head" .commit.tree.sha)" = "$tree" ]; then
    echo "$sha has the same code as pull request head $head, which passed CI." >&2
    echo true
    exit 0
  fi
  if [ -n "$base_date" ]; then
    for started in $runs; do
      if [ "$(date -u -d "$started" +%s)" -ge "$base_date" ]; then
        echo "CI passed on pull request head $head in a run started $started," \
          "after $base, the commit it was merged into, existed." >&2
        echo true
        exit 0
      fi
    done
  fi
done

echo "No passing CI run covers $sha; CI will run on it." >&2
echo false
