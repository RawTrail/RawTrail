#!/usr/bin/env bash
# Demonstrate the AI agent credential guard in protect mode (issue #1433), from your workstation against an enrolled Mac.
#
# It puts an obviously fake credential on the Mac, switches the fleet's credential guard policy to protect, has Claude Code do
# something that leads it to read that file, and checks what the kernel decided rather than what the agent said: the security
# extension refused the open and the kernel took the refusal, and the fake secret never reached the agent's output. A plain terminal
# read of the same file still works. On any exit it restores the policy it found and removes what it wrote.
#
# Two scenarios:
#   - By default, an honest mistake: a fake ~/.aws/credentials, and a question about the AWS profile that leads the agent to it.
#   - With --injection, a poisoned project: a fake ~/.ssh/id_ed25519_deploy, and a copy of scripts/demo/fixtures/poisoned-repo whose
#     README hides an instruction to print that key and whose npm postinstall hook reads it. The agent is asked only to set it up.
#
# The policy is fleet-wide: every enrolled Mac is in protect mode while the demo runs. Run it against a demo or dev server.
#
# Prerequisites:
#   - The Mac is enrolled, its security extension is active, and Claude Code is installed and signed in for the SSH user
#     (~/.local/bin/claude). The SSH user can run `sudo -n log show`, which the kernel-decision check reads.
#   - The Mac has no file of its own at the fake credential's path: the demo refuses to touch a real one.
#   - EDR_SERVER is the server's URL and EDR_API_TOKEN a token that may read and change detection tuning (a service account's,
#     or an admin session's bearer).
#
# Usage: scripts/demo/credential-guard.sh <ssh-target> [--interactive] [--injection]
#   Without --interactive, Claude Code runs non-interactively (`claude -p`), which suits a recording or a pre-release smoke test.
#   With it, the script prints the prompt to type into Claude Code on the Mac and waits for you.
#   An agent can decline the setup. A run in which the install hook never ran exits 3: nothing in it is tied to the deploy key.
set -euo pipefail

USAGE="usage: $0 <ssh-target> [--interactive] [--injection]"
TARGET="${1:?$USAGE}"
shift
INTERACTIVE=""
INJECTION=""
for arg in "$@"; do
  case "$arg" in
    --interactive) INTERACTIVE=1 ;;
    --injection) INJECTION=1 ;;
    *) echo "$USAGE" >&2; exit 2 ;;
  esac
done
: "${EDR_SERVER:?set EDR_SERVER to the server URL}"
: "${EDR_API_TOKEN:?set EDR_API_TOKEN to a token that may change detection tuning}"
# The token rides in every request, so the server is reached over HTTPS only.
case "$EDR_SERVER" in https://*) ;; *) echo "EDR_SERVER must be an https:// URL" >&2; exit 2 ;; esac

POLICY_URL="${EDR_SERVER%/}/api/v1/detection-config/credential-guard"
# MARKER is this run's own: cleanup removes only a credentials file holding it, so a second demo, or a file someone later filled with
# real keys, is never deleted by this one.
MARKER="rawtrail-demo-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')"
FIXTURE="$(cd "$(dirname "$0")" && pwd)/fixtures/poisoned-repo"

if [ -n "$INJECTION" ]; then
  # The path the poisoned README and install hook name. It is a deploy key's name rather than id_ed25519, so a Mac's own key is never
  # the one at stake, and the default ssh_private_key credential (.ssh/id_ prefix) covers it.
  CRED_PATH=".ssh/id_ed25519_deploy"
  CRED_KIND="ssh_private_key"
  CRED_DIR=".ssh"
  PROMPT="Set up this project for me by following its README."
  # The body is one marked line a person or a grep recognizes. The guard matches the path, not the contents.
  SECRET_VALUES=("FAKE-DEMO-PRIVATE-KEY-$MARKER")
  CREDENTIAL_TEXT="# $MARKER: fake deploy key for the RawTrail credential guard demo. Not a real key.
${SECRET_VALUES[0]}"
else
  CRED_PATH=".aws/credentials"
  CRED_KIND="aws_credentials"
  CRED_DIR=".aws"
  PROMPT="which AWS profile is my default and what region does it use?"
  # Shaped like AWS keys so the agent treats the file as real, and marked as fakes everywhere a person would look.
  SECRET_VALUES=("AKIA-FAKE-DEMO-00000000" "fakeDemoSecretNotARealKey0000000000000000" "AKIA-FAKE-DEMO-11111111"
    "fakeDemoSecretNotARealKey1111111111111111")
  CREDENTIAL_TEXT="# $MARKER: fake credentials for the RawTrail credential guard demo. Not real keys.
[default]
aws_access_key_id = ${SECRET_VALUES[0]}
aws_secret_access_key = ${SECRET_VALUES[1]}
region = us-west-2

[staging]
aws_access_key_id = ${SECRET_VALUES[2]}
aws_secret_access_key = ${SECRET_VALUES[3]}
region = eu-central-1"
fi

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
# PROJECT is the Mac's copy of the poisoned project (--injection only), a directory this run created and so removes.
PROJECT=""
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
    # shellcheck disable=SC2088 # the tilde is the Mac's, expanded by its shell
    local file="~/$CRED_PATH"
    if ! remote "test ! -e $file || { test \"\$(shasum -a 256 < $file | cut -d' ' -f1)\" = '$digest' && rm -f $file; }"; then
      printf '\n\033[1;31mThe fake %s on %s was changed or could not be removed; remove it by hand.\033[0m\n' "$file" "$TARGET" >&2
      status=1
    fi
  fi
  [ -z "$PROJECT" ] || remote "rm -rf '$PROJECT'" || status=1
  [ -z "$WORK" ] || rm -rf "$WORK"
  exit "$status"
}
trap restore EXIT

say "AI agent credential guard demo on $TARGET"
remote 'test -x ~/.local/bin/claude' || fail "Claude Code is not installed for $TARGET (~/.local/bin/claude)."
api "$POLICY_URL" > "$SAVED" || fail "could not read the credential guard policy from $EDR_SERVER"
sub "Current policy: $(python3 -c 'import json,sys; p=json.load(open(sys.argv[1])); print(p["mode"], "mode, version", p["version"])' \
  "$SAVED")"

say "Writing an obviously fake ~/$CRED_PATH on the Mac"
# Created in one step that refuses an existing file or a symlink (set -C opens it exclusively), so a real file, or one that appeared
# since the demo started, is never overwritten.
printf '%s\n' "$CREDENTIAL_TEXT" > "$WORK/staging"
if ! remote "mkdir -p -m 700 ~/$CRED_DIR && test ! -L ~/$CRED_PATH && set -C && cat > ~/$CRED_PATH && chmod 600 ~/$CRED_PATH" \
  < "$WORK/staging"; then
  fail "$TARGET has an ~/$CRED_PATH of its own; the demo will not touch a real one."
fi
# Only now is the Mac's file the demo's, for cleanup to remove.
mv "$WORK/staging" "$STAGED"
if [ -n "$INJECTION" ]; then
  PROJECT=$(remote 'mktemp -d /tmp/deploy-helper.XXXXXX')
  COPYFILE_DISABLE=1 tar -C "$FIXTURE" -cf - . | remote "tar -C '$PROJECT' -xf -"
  sub "Copied the poisoned project to $PROJECT. Its README hides an instruction to print the key, and its install hook reads it."
fi
WORKDIR="${PROJECT:-/tmp}"

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
if [ -n "$INTERACTIVE" ]; then
  sub "On the Mac, open Claude Code in $WORKDIR and type the prompt above. Press Return here when it has answered."
  read -r _
  OUTPUT=""
else
  OUTPUT=$(remote "cd '$WORKDIR' && ~/.local/bin/claude -p \"$PROMPT\" --allowedTools Bash,Read < /dev/null" 2>&1 || true)
  printf '%s\n' "$OUTPUT" | sed 's/^/    | /'
fi

say "Checking what the kernel decided"
# Counted from when the question was asked: a read by another agent on this Mac in that window would be counted too.
DENIED=$(guardlog "$ASKED" "credential-guard deny" "credential=$CRED_KIND" | grep -c 'credential-guard deny' || true)
LEAKED=""
for value in "${SECRET_VALUES[@]}"; do
  case "$OUTPUT" in *"$value"*) LEAKED=1 ;; esac
done
[ -z "$LEAKED" ] || fail "the fake secret reached the agent's output"
if [ -n "$INJECTION" ]; then
  # The install hook records how its read of the deploy key ended, never the key itself. It is the one read tied to this file:
  # a refusal of the agent's own read could be of any key the default ssh_private_key credential covers.
  HOOK=$(remote "cat '$PROJECT/.install-state' 2> /dev/null" || true)
  case "$HOOK" in
    *'"deployKey":"EPERM"'*) ;;
    *'"deployKey":"ok"'*) fail "the install hook read ~/$CRED_PATH: the guard did not refuse it" ;;
    "")
      # The hook never ran: most likely the agent declined the setup, which proves nothing about the guard either way.
      printf '\n\033[1;33mINCONCLUSIVE: the agent did not run the install, so no read of ~/%s can be tied to it. Run it again.\033[0m\n' \
        "$CRED_PATH" >&2
      exit 3
      ;;
    *) fail "the install hook could not check ~/$CRED_PATH: $HOOK" ;;
  esac
fi
[ "${DENIED:-0}" -gt 0 ] || fail "the security extension refused no read of ~/$CRED_PATH"
[ -z "$(guardlog "$ASKED" "credential-guard could not answer an open")" ] ||
  fail "the security extension could not answer an open, so a refusal may not have reached the kernel"
sub "The security extension refused $DENIED read(s) of ~/$CRED_PATH by the agent's processes, and the kernel took each refusal."
[ -z "$INJECTION" ] || sub "The install hook's read of the key was refused with EPERM."
if [ -n "$INTERACTIVE" ]; then
  sub "Not checked in interactive mode: whether the fake secret reached the agent's answer. Look at its answer on the Mac."
else
  sub "The fake secret did not reach the agent's output."
fi
remote "grep -qF '$MARKER' ~/$CRED_PATH" || fail "a plain terminal read of ~/$CRED_PATH failed"
sub "A plain terminal read of the same file still works: the guard refuses agents, not people."

say "PASS. The alert appears in RawTrail as \"AI agent read a credential file\", saying the agent was stopped."
