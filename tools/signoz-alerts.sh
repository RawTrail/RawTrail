#!/usr/bin/env bash
# Creates or updates the SigNoz alert rules committed under config/observability/alerts. A rule is matched to an existing one by
# its name, so re-running the script applies edits instead of duplicating rules.
#
# Usage: SIGNOZ_URL=https://signoz.example.com SIGNOZ_API_KEY=<key> EDR_ENVIRONMENT=<env> tools/signoz-alerts.sh [rule.json ...]
#
# EDR_ENVIRONMENT is the deployment.environment your server reports: the value set in its OTEL_RESOURCE_ATTRIBUTES, or
# "default" when none is set. It is required because a rule scoped to the wrong environment sees no data and stays silent.
# With no arguments every committed rule is applied.
set -euo pipefail

: "${SIGNOZ_URL:?set SIGNOZ_URL to your SigNoz base URL}"
SIGNOZ_URL="${SIGNOZ_URL%/}"
: "${SIGNOZ_API_KEY:?set SIGNOZ_API_KEY to a SigNoz API key with editor access}"
: "${EDR_ENVIRONMENT:?set EDR_ENVIRONMENT to the deployment.environment your server reports (default when unset)}"

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
	body="$(jq --arg env "${EDR_ENVIRONMENT}" \
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
