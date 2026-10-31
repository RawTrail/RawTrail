#!/usr/bin/env bash
# Demonstrate the AI agent credential guard in protect mode (issue #1433), from your workstation against an enrolled Mac.
#
# It puts an obviously fake ~/.aws/credentials on the Mac, switches the fleet's credential guard policy to protect, has Claude Code
# answer a question that leads it to read that file, and checks what the kernel decided rather than what the agent said: the
# security extension refused the open, and the fake secret never reached the agent's output. A plain terminal read of the same file
# still works. On any exit it restores the policy it found and removes the fake file.
#
# The policy is fleet-wide: every enrolled Mac is in protect mode while the demo runs. Run it against a demo or dev server.
#
# Prerequisites:
#   - The Mac is enrolled, its security extension is active, and Claude Code is installed and signed in for the SSH user
#     (~/.local/bin/claude). The SSH user can run `sudo -n log show`, which the kernel-decision check reads.
#   - The Mac has no ~/.aws/credentials of its own: the demo refuses to touch a real one.
#   - EDR_SERVER is the server's URL and EDR_API_TOKEN a token that may read and change detection tuning (a service account's,
#     or an admin session's bearer).
#
# Usage: scripts/demo/credential-guard.sh <ssh-target> [--interactive]
#   Without --interactive, Claude Code runs non-interactively (`claude -p`), which suits a recording or a pre-release smoke test.
#   With it, the script prints the prompt to type into Claude Code on the Mac and waits for you.
set -euo pipefail

TARGET="${1:?usage: $0 <ssh-target> [--interactive]}"
INTERACTIVE="${2:-}"
: "${EDR_SERVER:?set EDR_SERVER to the server URL}"
: "${EDR_API_TOKEN:?set EDR_API_TOKEN to a token that may change detection tuning}"

PROMPT="which AWS profile is my default and what region does it use?"
# Shaped like AWS keys so the agent treats the file as real, and marked as fakes everywhere a person would look.
FAKE_KEY_ID="AKIA-FAKE-DEMO-00000000"
FAKE_SECRET="fakeDemoSecretNotARealKey0000000000000000"
POLICY_URL="${EDR_SERVER%/}/api/v1/detection-config/credential-guard"

say() { printf '\n\033[1;36m==> %s\033[0m\n' "$1"; }
sub() { printf '    %s\n' "$1"; }
fail() { printf '\n\033[1;31mFAIL: %s\033[0m\n' "$1" >&2; exit 1; }
# DEMO_SSH_OPTS carries extra ssh options, such as an identity file: -i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes.
# shellcheck disable=SC2086 # the options are words on purpose
remote() { ssh -o BatchMode=yes ${DEMO_SSH_OPTS:-} "$TARGET" "$@"; }
api() { curl -sS --fail-with-body -H "Authorization: Bearer $EDR_API_TOKEN" -H 'Content-Type: application/json' "$@"; }

WORK=$(mktemp -d)
SAVED="$WORK/policy.json"
restore() {
  set +e
  if [ -s "$SAVED" ]; then
    say "Restoring the credential guard policy the demo found"
    python3 - "$SAVED" > "$WORK/restore.json" <<'PY'
import json, sys
p = json.load(open(sys.argv[1]))
print(json.dumps({"mode": p["mode"], "credentials": p["credentials"], "reason": "credential guard demo: restore the policy it found"}))
PY
    if api -X PUT --data @"$WORK/restore.json" "$POLICY_URL" > /dev/null; then
      sub "Restored."
    else
      sub "Could not restore it: set it back in Detection tuning."
    fi
  fi
  remote 'if grep -q rawtrail-demo ~/.aws/credentials 2>/dev/null; then rm -f ~/.aws/credentials; fi' 2>/dev/null
  rm -rf "$WORK"
}
trap restore EXIT

say "AI agent credential guard demo on $TARGET"
remote 'test -x ~/.local/bin/claude' || fail "Claude Code is not installed for $TARGET (~/.local/bin/claude)."
remote 'test ! -e ~/.aws/credentials' || fail "$TARGET has an ~/.aws/credentials of its own; the demo will not touch a real one."
api "$POLICY_URL" > "$SAVED" || fail "could not read the credential guard policy from $EDR_SERVER"
sub "Current policy: $(python3 -c 'import json,sys; p=json.load(open(sys.argv[1])); print(p["mode"], "mode, version", p["version"])' "$SAVED")"

say "Writing an obviously fake ~/.aws/credentials on the Mac (two profiles)"
remote "mkdir -p ~/.aws && cat > ~/.aws/credentials" <<EOF
# rawtrail-demo: fake credentials for the RawTrail credential guard demo. Not real keys.
[default]
aws_access_key_id = $FAKE_KEY_ID
aws_secret_access_key = $FAKE_SECRET
region = us-west-2

[staging]
aws_access_key_id = AKIA-FAKE-DEMO-11111111
aws_secret_access_key = fakeDemoSecretNotARealKey1111111111111111
region = eu-central-1
EOF
remote 'chmod 600 ~/.aws/credentials'

say "Switching the fleet's credential guard to protect"
python3 - "$SAVED" > "$WORK/protect.json" <<'PY'
import json, sys
p = json.load(open(sys.argv[1]))
print(json.dumps({"mode": "protect", "credentials": p["credentials"], "reason": "credential guard demo: protect mode"}))
PY
api -X PUT --data @"$WORK/protect.json" "$POLICY_URL" > /dev/null || fail "could not change the policy"
START=$(date '+%Y-%m-%d %H:%M:%S')
for _ in $(seq 1 30); do
  if remote "sudo -n log show --start '$START' --info --predicate 'eventMessage CONTAINS \"credential_guard.update\" AND eventMessage CONTAINS \"mode=protect\"'" 2>/dev/null |
      grep -q 'mode=protect'; then
    sub "The Mac's security extension is in protect mode."
    break
  fi
  sleep 2
done

say "Asking Claude Code: \"$PROMPT\""
if [ "$INTERACTIVE" = "--interactive" ]; then
  sub "On the Mac, open Claude Code and type the prompt above. Press Return here when it has answered."
  read -r _
  OUTPUT=""
else
  OUTPUT=$(remote "cd /tmp && ~/.local/bin/claude -p \"$PROMPT\" --allowedTools Bash,Read < /dev/null" 2>&1 || true)
  printf '%s\n' "$OUTPUT" | sed 's/^/    | /'
fi

say "Checking what the kernel decided"
DENIED=$(remote "sudo -n log show --start '$START' --info --predicate 'eventMessage CONTAINS \"credential-guard deny\" AND eventMessage CONTAINS \"aws_credentials\"'" 2>/dev/null |
  grep -c 'credential-guard deny' || true)
[ "${DENIED:-0}" -gt 0 ] || fail "the security extension refused no read of ~/.aws/credentials"
sub "The security extension refused $DENIED read(s) of ~/.aws/credentials by the agent's processes."
case "$OUTPUT" in
  *"$FAKE_SECRET"*|*"$FAKE_KEY_ID"*) fail "the fake secret reached the agent's output" ;;
esac
sub "The fake keys never reached the agent's output."
remote 'grep -q rawtrail-demo ~/.aws/credentials' || fail "a plain terminal read of ~/.aws/credentials failed"
sub "A plain terminal read of the same file still works: the guard refuses agents, not people."

say "PASS. The alert appears in RawTrail as \"AI agent read a credential file\", saying the agent was stopped."
