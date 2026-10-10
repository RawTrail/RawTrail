#!/usr/bin/env bash
# Creates or updates the SigNoz alert rules committed under config/observability/alerts. A rule is matched to an existing one by
# its name, so re-running the script applies edits instead of duplicating rules.
#
# Usage: SIGNOZ_URL=https://signoz.example.com SIGNOZ_API_KEY=<key> [EDR_ENVIRONMENT=prod] tools/signoz-alerts.sh [rule.json ...]
#
# The rules filter on deployment.environment = 'prod'. EDR_ENVIRONMENT substitutes your server's environment name.
# With no arguments every committed rule is applied.
set -euo pipefail

: "${SIGNOZ_URL:?set SIGNOZ_URL to your SigNoz base URL}"
SIGNOZ_URL="${SIGNOZ_URL%/}"
: "${SIGNOZ_API_KEY:?set SIGNOZ_API_KEY to a SigNoz API key with editor access}"
environment="${EDR_ENVIRONMENT:-prod}"

rules_dir="$(cd "$(dirname "$0")/.." && pwd)/config/observability/alerts"
if [ "$#" -gt 0 ]; then
	files=("$@")
else
	files=("$rules_dir"/*.json)
fi

api() {
	curl -fsS -H "SIGNOZ-API-KEY: ${SIGNOZ_API_KEY}" -H 'Content-Type: application/json' "$@"
}

existing="$(api "${SIGNOZ_URL}/api/v1/rules")"
for file in "${files[@]}"; do
	name="$(jq -r .alert "$file")"
	body="$(jq --arg env "$environment" \
		'walk(if type == "string" then sub("deployment.environment = '\''prod'\''"; "deployment.environment = '\''\($env)'\''") else . end)' \
		"$file")"
	id="$(jq -r --arg name "$name" '.data.rules[] | select(.alert == $name) | .id' <<<"$existing" | head -n 1)"
	if [ -n "$id" ]; then
		api -X PUT "${SIGNOZ_URL}/api/v1/rules/${id}" --data "$body" >/dev/null
		echo "updated: ${name}"
	else
		api -X POST "${SIGNOZ_URL}/api/v1/rules" --data "$body" >/dev/null
		echo "created: ${name}"
	fi
done
