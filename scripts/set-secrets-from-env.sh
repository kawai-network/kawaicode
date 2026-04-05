#!/usr/bin/env bash
# set-secrets-from-env.sh
# Reads key=value pairs from .env and sets them as GitHub repository secrets.
# Requires: gh CLI authenticated with repo access.
#
# Usage:
#   ./scripts/set-secrets-from-env.sh            # uses .env in project root
#   ./scripts/set-secrets-from-env.sh .env.prod  # uses a custom env file

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

ENV_FILE="${1:-${PROJECT_ROOT}/.env}"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "Error: env file not found: $ENV_FILE" >&2
  exit 1
fi

if ! command -v gh &>/dev/null; then
  echo "Error: gh CLI is required but not installed." >&2
  exit 1
fi

# Verify gh is authenticated
if ! gh auth status &>/dev/null; then
  echo "Error: gh is not authenticated. Run 'gh auth login' first." >&2
  exit 1
fi

# Detect repo from git remote
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null)
if [[ -z "$REPO" ]]; then
  echo "Error: could not detect repository. Set -R or run from a git repo." >&2
  exit 1
fi
echo "Using repository: $REPO"
echo ""

GH_REPO="-R $REPO"

count=0
while IFS= read -r line || [[ -n "$line" ]]; do
  # Skip empty lines and comments
  [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue

  # Extract key=value (trim whitespace)
  if [[ "$line" =~ ^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)=(.*)[[:space:]]*$ ]]; then
    key="${BASH_REMATCH[1]}"
    value="${BASH_REMATCH[2]}"

    # Remove optional surrounding quotes from value
    value="${value#\"}"
    value="${value%\"}"
    value="${value#\'}"
    value="${value%\'}"

    echo "Setting secret: $key"
    echo -n "$value" | gh secret set "$key" $GH_REPO
    ((count++))
  fi
done < "$ENV_FILE"

echo ""
echo "Done! Set $count secret(s) from $ENV_FILE"
