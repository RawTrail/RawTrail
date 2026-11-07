#!/bin/sh
#
# packaging/pkg/build.test.sh
#
# Tests build.sh's staple retries. Stapling and validating each ask Apple's ticket service for the ticket, and either request can
# fail with exit 68 right after the notary accepted the pkg (v0.9.0-rc.1 and rc.2 failed that way), so build.sh retries each a
# bounded number of times. Runs the staple block as build.sh has it, with xcrun and sleep as shell functions: no Apple tools, no
# network, no waiting.
#
# Run: sh packaging/pkg/build.test.sh

set -eu

# spec:release-packaging/notarization-and-stapling/a-failed-ticket-fetch-is-retried
block=$(sed -n '/^echo "==> stapling ticket"/,/^retry_apple "validating/p' "$(dirname "$0")/build.sh")
case $block in
    *'retry_apple "validating'*) ;;
    *) echo "FAIL: the staple block was not found in build.sh"; exit 1 ;;
esac

# run_staple runs the block with the first $1 staple and $2 validate requests failing, and prints how many of each were made, how
# many calls were neither, and the block's exit status. An exit trap prints them, so a block that gives up with exit is reported
# with its real status. A call that is not `stapler staple -v` or `stapler validate -v` on the package is counted as bad and fails.
run_staple() {
    (
        staple_fails=$1
        validate_fails=$2
        staples=0
        validates=0
        bad=0
        # shellcheck disable=SC2329  # called by the eval'd block
        xcrun() {
            if [ "$*" = "stapler staple -v test.pkg" ]; then
                staples=$((staples + 1))
                [ "$staples" -gt "$staple_fails" ]
            elif [ "$*" = "stapler validate -v test.pkg" ]; then
                validates=$((validates + 1))
                [ "$validates" -gt "$validate_fails" ]
            else
                bad=$((bad + 1))
                return 99
            fi
        }
        # shellcheck disable=SC2329  # called by the eval'd block
        sleep() { :; }
        PKG_OUT=test.pkg
        export PKG_OUT
        trap 'echo "$staples $validates $bad $?"' EXIT
        eval "$block" >/dev/null 2>&1
    ) || true
}

failures=0
check() {
    got=$(run_staple "$1" "$2")
    if [ "$got" = "$3" ]; then echo "ok: $4"; else echo "FAIL: $4: got '$got', want '$3'"; failures=$((failures + 1)); fi
}

# Columns: staple requests, validate requests, bad calls, exit status.
check 0 0 "1 1 0 0" "a ticket stapled and validated at once"
check 4 0 "5 1 0 0" "four failed staples are retried, and the fifth staples"
# spec:release-packaging/notarization-and-stapling/a-failed-validation-is-retried-on-its-own
check 0 2 "1 3 0 0" "a failed validate is retried on its own, without stapling again"
check 5 0 "5 0 0 1" "five failed staples fail the build without validating"
check 0 5 "1 5 0 1" "five failed validates fail the build"

[ "$failures" -eq 0 ] || exit 1
