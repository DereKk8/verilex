#!/usr/bin/env bash
# Check an Author prompts comment, then create or update the PR's one comment.
# Usage: post.sh <pr-number> <comment-file>   (run from the repository root)
set -euo pipefail
pr=${1:?usage: post.sh <pr-number> <comment-file>}
file=${2:?usage: post.sh <pr-number> <comment-file>}
"$(dirname "$0")/check.sh" "$file"
id=$(gh api --paginate "repos/{owner}/{repo}/issues/$pr/comments" \
  --jq '.[] | select(.body | startswith("## Author prompts")) | .id' | head -n1)
if [ -n "$id" ]; then
  gh api -X PATCH "repos/{owner}/{repo}/issues/comments/$id" -F body=@"$file" >/dev/null
  echo "updated author prompts comment $id on PR $pr"
else
  gh pr comment "$pr" --body-file "$file"
fi
