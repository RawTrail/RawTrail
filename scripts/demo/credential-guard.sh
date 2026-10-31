#!/usr/bin/env bash
# Demonstrate the AI agent credential guard in protect mode (issue #1433), from your workstation against an enrolled Mac.
#
# It puts an obviously fake ~/.aws/credentials on the Mac, switches the fleet's credential guard policy to protect, has Claude Code
# answer a question that leads it to read that file, and checks what the kernel decided rather than what the agent said: the
# security extension refused the open and the kernel took the refusal, and the fake keys never reached the agent's output. A plain
# terminal read of the same file still works. On any exit it restores the policy it found and removes the fake file it wrote.
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
# The token rides in every request, so the server is reached over HTTPS only.
case "$EDR_SERVER" in https://*) ;; *) echo "EDR_SERVER must be an https:// URL" >&2; exit 2 ;; esac

PROMPT="which AWS profile is my default and what region does it use?"
# Shaped like AWS keys so the agent treats the file as real, and marked as fakes everywhere a person would look.
FAKE_KEY_ID="AKIA-FAKE-DEMO-00000000"
FAKE_SECRET="fakeDemoSecretNotARealKey0000000000000000"
STAGING_KEY_ID="AKIA-FAKE-DEMO-11111111"
STAGING_SECRET="fakeDemoSecretNotARealKey1111111111111111"
POLICY_URL="${EDR_SERVER%/}/api/v1/detection-config/credential-guard"
# MARKER is this run's own: cleanup removes only a credentials file holding it, so a second demo, or a file someone later filled with
# real keys, is never deleted by this one.
MARKER="rawtrail-demo-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')"

say() { printf '\n\033[1;36m==> %s\033[0m\n' "$1"; }
sub() { printf '    %s\n' "$1"; }
fail() { printf '\n\033[1;31mFAIL: %s\033[0m\n' "$1" >&2; exit 1; }
# DEMO_SSH_OPTS carries extra ssh options, such as an identity file: -i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes.
# shellcheck disable=SC2086 # the options are words on purpose
remote() { ssh -o BatchMode=yes ${DEMO_SSH_OPTS:-} "$TARGET" "$@"; }
api() { curl -sS --fail-with-body -H "Authorization: Bearer $EDR_API_TOKEN" -H 'Content-Type: application/json' "$@"; }
# guardlog prints the security extension's log lines on the Mac since a time the Mac gave, that contain every one of the words.
guardlog() {
  local since="$1" first="$2"
  shift
  local predicate='process CONTAINS "io.rawtrail.securityextension"'
  for word in "$@"; do predicate="$predicate AND eventMessage CONTAINS \"$word\""; done
  remote "sudo -n log show --start '$since' --info --predicate '$predicate'" 2>/dev/null | grep -F "$first" || true
}

WORK=$(mktemp -d)
SAVED="$WORK/policy.json"
# STAGED is the fake file exactly as written, and its digest the one test cleanup removes the Mac's file by: a file that has changed
# since, even one still holding the marker, is left alone.
STAGED="$WORK/credentials"
# CHANGED is the version this run's protect write stored, set once the write succeeded: only then is there a change to undo.
CHANGED=""
restore() {
  local status=$?
  set +e
  if [ -n "$CHANGED" ]; then
    say "Restoring the credential guard policy the demo found"
    python3 - "$SAVED" "$CHANGED" > "$WORK/restore.json" <<'PY'
import json, sys
p = json.load(open(sys.argv[1]))
print(json.dumps({"mode": p["mode"], "credentials": p["credentials"], "expected_version": int(sys.argv[2]),
                  "reason": "credential guard demo: restore the policy it found"}))
PY
    if api -X PUT --data @"$WORK/restore.json" "$POLICY_URL" > /dev/null; then
      sub "Restored."
    else
      # A refusal as out of date means someone changed the policy since: theirs stands, and the demo's starting point is kept.
      local kept
      if kept=$(mktemp "${TMPDIR:-/tmp}/credential-guard-demo-policy.XXXXXX") && cp "$SAVED" "$kept"; then
        printf '\n\033[1;31mThe policy was not restored. The one the demo found is saved at %s; set it back in Detection tuning.\033[0m\n' \
          "$kept" >&2
      else
        printf '\n\033[1;31mThe policy was not restored. The one the demo found is at %s; set it back in Detection tuning.\033[0m\n' \
          "$SAVED" >&2
        WORK=""
      fi
      status=1
    fi
  fi
  if [ -s "$STAGED" ]; then
    local digest
    digest=$(shasum -a 256 < "$STAGED" | cut -d' ' -f1)
    if ! remote "test ! -e ~/.aws/credentials || { test \"\$(shasum -a 256 < ~/.aws/credentials | cut -d' ' -f1)\" = '$digest' && rm -f ~/.aws/credentials; }"; then
      printf '\n\033[1;31mThe fake ~/.aws/credentials on %s was changed or could not be removed; remove it by hand.\033[0m\n' "$TARGET" >&2
      status=1
    fi
  fi
  [ -z "$WORK" ] || rm -rf "$WORK"
  exit "$status"
}
trap restore EXIT

say "AI agent credential guard demo on $TARGET"
remote 'test -x ~/.local/bin/claude' || fail "Claude Code is not installed for $TARGET (~/.local/bin/claude)."
api "$POLICY_URL" > "$SAVED" || fail "could not read the credential guard policy from $EDR_SERVER"
sub "Current policy: $(python3 -c 'import json,sys; p=json.load(open(sys.argv[1])); print(p["mode"], "mode, version", p["version"])' "$SAVED")"

say "Writing an obviously fake ~/.aws/credentials on the Mac (two profiles)"
# Created in one step that refuses an existing file or a symlink (set -C opens it exclusively), so a real file, or one that appeared
# since the demo started, is never overwritten.
cat > "$WORK/staging" <<CREDENTIALS
# $MARKER: fake credentials for the RawTrail credential guard demo. Not real keys.
[default]
aws_access_key_id = $FAKE_KEY_ID
aws_secret_access_key = $FAKE_SECRET
region = us-west-2

[staging]
aws_access_key_id = $STAGING_KEY_ID
aws_secret_access_key = $STAGING_SECRET
region = eu-central-1
CREDENTIALS
if ! remote "mkdir -p ~/.aws && test ! -L ~/.aws/credentials && set -C && cat > ~/.aws/credentials && chmod 600 ~/.aws/credentials" \
  < "$WORK/staging"; then
  fail "$TARGET has an ~/.aws/credentials of its own; the demo will not touch a real one."
fi
# Only now is the Mac's file the demo's, for cleanup to remove.
mv "$WORK/staging" "$STAGED"

say "Switching the fleet's credential guard to protect"
# The Mac's clock, before the change, so its log is read from a time it gave and nothing it writes after the change is missed.
SINCE=$(remote "date '+%Y-%m-%d %H:%M:%S'")
python3 - "$SAVED" > "$WORK/protect.json" <<'PY'
import json, sys
p = json.load(open(sys.argv[1]))
print(json.dumps({"mode": "protect", "credentials": p["credentials"], "expected_version": p["version"],
                  "reason": "credential guard demo: protect mode"}))
PY
if api -X PUT --data @"$WORK/protect.json" "$POLICY_URL" > "$WORK/protected.json"; then
  CHANGED=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["policy"]["version"])' "$WORK/protected.json")
else
  # A write can land and its answer be lost. The policy one past the version read, in protect mode, is that write, so it is restored.
  CHANGED=$(api "$POLICY_URL" | python3 -c '
import json, sys
p, read = json.load(sys.stdin), int(sys.argv[1])
print(p["version"] if p["version"] == read + 1 and p["mode"] == "protect" else "")' \
    "$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$SAVED")" || true)
  fail "could not change the policy (it is refused when someone changed it since the demo read it)"
fi
READY=""
for _ in $(seq 1 30); do
  if [ -n "$(guardlog "$SINCE" "credential_guard.update" "version=$CHANGED " "mode=protect")" ]; then
    READY=1
    break
  fi
  sleep 2
done
[ -n "$READY" ] || fail "the Mac's security extension did not apply protect mode within a minute"
sub "The Mac's security extension is in protect mode (policy version $CHANGED)."

say "Asking Claude Code: \"$PROMPT\""
ASKED=$(remote "date '+%Y-%m-%d %H:%M:%S'")
if [ "$INTERACTIVE" = "--interactive" ]; then
  sub "On the Mac, open Claude Code and type the prompt above. Press Return here when it has answered."
  read -r _
  OUTPUT=""
else
  OUTPUT=$(remote "cd /tmp && ~/.local/bin/claude -p \"$PROMPT\" --allowedTools Bash,Read < /dev/null" 2>&1 || true)
  printf '%s\n' "$OUTPUT" | sed 's/^/    | /'
fi

say "Checking what the kernel decided"
# Counted from when the question was asked: a read by another agent on this Mac in that window would be counted too.
DENIED=$(guardlog "$ASKED" "credential-guard deny" "credential=aws_credentials" | grep -c 'credential-guard deny' || true)
[ "${DENIED:-0}" -gt 0 ] || fail "the security extension refused no read of ~/.aws/credentials"
[ -z "$(guardlog "$ASKED" "credential-guard could not answer an open")" ] ||
  fail "the security extension could not answer an open, so a refusal may not have reached the kernel"
sub "The security extension refused $DENIED read(s) of ~/.aws/credentials by the agent's processes, and the kernel took each refusal."
if [ "$INTERACTIVE" = "--interactive" ]; then
  sub "Not checked in interactive mode: whether the fake keys reached the agent's answer. Look at its answer on the Mac."
else
  for value in "$FAKE_KEY_ID" "$FAKE_SECRET" "$STAGING_KEY_ID" "$STAGING_SECRET"; do
    case "$OUTPUT" in *"$value"*) fail "a fake key reached the agent's output" ;; esac
  done
  sub "None of the fake keys reached the agent's output."
fi
remote "grep -qF '$MARKER' ~/.aws/credentials" || fail "a plain terminal read of ~/.aws/credentials failed"
sub "A plain terminal read of the same file still works: the guard refuses agents, not people."

say "PASS. The alert appears in RawTrail as \"AI agent read a credential file\", saying the agent was stopped."
