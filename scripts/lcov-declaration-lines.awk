# lcov-declaration-lines.awk gives each function's declaration line the line hit it lacks in vitest's LCOV report.
#
# vitest's v8 provider records a function (FN, FNDA) but no line hit (DA) for the line that declares it, while the Playwright report
# records that line as DA:<line>,0 whenever the E2E run did not reach the function. SonarCloud merges the two, so every declaration
# line read as uncovered, however often vitest called the function: a PR that only changed a component's signature failed the
# new-code coverage gate on that line alone. This adds DA:<line>,<calls> from the function's own FNDA count for each declaration line
# without a DA, so the line counts as covered exactly when its function ran under vitest, and as uncovered when it never did.
#
# FN is FN:<line>,<name> or FN:<line>,<end line>,<name>; FNDA is FNDA:<calls>,<name>. Lines that already have a DA are left alone.
#
# Usage: awk -f scripts/lcov-declaration-lines.awk lcov.info > lcov.fixed.info

/^SF:/ {
  split("", declared)
  split("", calls)
  split("", hit)
}

/^FN:/ {
  n = split(substr($0, 4), field, ",")
  declared[field[n]] = field[1]
}

/^FNDA:/ {
  comma = index($0, ",")
  calls[substr($0, comma + 1)] = substr($0, 6, comma - 6)
}

/^DA:/ {
  split(substr($0, 4), field, ",")
  hit[field[1]] = 1
}

/^end_of_record/ {
  for (name in declared) {
    line = declared[name]
    if (!(line in hit) && (name in calls)) {
      print "DA:" line "," calls[name]
      hit[line] = 1
    }
  }
}

{ print }
