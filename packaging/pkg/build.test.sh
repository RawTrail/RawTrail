#!/bin/sh
#
# packaging/pkg/build.test.sh
#
# Tests build.sh's staple retry. Apple's ticket fetch fails intermittently with exit 68 right after the notary accepted the pkg
# (v0.9.0-rc.1 failed twice that way), so build.sh retries it a bounded number of times. Runs the staple block as build.sh has it,
# with xcrun and sleep as shell functions: no Apple tools, no network, no waiting.
#
# Run: sh packaging/pkg/build.test.sh

set -eu

# spec:release-packaging/notarization-and-stapling/a-failed-ticket-fetch-is-retried
block=$(sed -n '/^echo "==> stapling ticket"/,/^xcrun stapler validate/p' "$(dirname "$0")/build.sh")
[ -n "$block" ] || { echo "FAIL: the staple block was not found in build.sh"; exit 1; }

# run_staple runs the block with the first $1 staple calls failing and prints how many were made, whether the package was validated,
# and the block's exit status. An exit trap prints them, so a block that gives up with exit is reported with its real status.
run_staple() {
    (
        fails=$1
        calls=0
        validated=no
        # shellcheck disable=SC2329  # called by the eval'd block
        xcrun() {
            if [ "$2" = "validate" ]; then validated=yes; return 0; fi
            calls=$((calls + 1))
            [ "$calls" -gt "$fails" ]
        }
        # shellcheck disable=SC2329  # called by the eval'd block
        sleep() { :; }
        PKG_OUT=test.pkg
        export PKG_OUT
        trap 'echo "$calls $validated $?"' EXIT
        eval "$block" >/dev/null 2>&1
    ) || true
}

failures=0
check() {
    got=$(run_staple "$1")
    if [ "$got" = "$2" ]; then echo "ok: $3"; else echo "FAIL: $3: got '$got', want '$2'"; failures=$((failures + 1)); fi
}

check 0 "1 yes 0" "a ticket fetched at once is stapled and validated"
check 4 "5 yes 0" "four failed fetches are retried, and the fifth staples"
check 5 "5 no 1" "five failed fetches fail the build without validating"

[ "$failures" -eq 0 ] || exit 1
