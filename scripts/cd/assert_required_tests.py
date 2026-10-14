#!/usr/bin/env python3
"""Assert that required tests in a `go test -json` run actually passed.

Usage: assert_required_tests.py --require REGEX [--require REGEX ...] <go-test-json>

A gate that matches zero tests, or whose REQUIRED tests all skipped, exits 0
from `go test` — a vacuous green. This turns both into a hard failure
(TRANSITION_PLAN.md §3 rule 8: required tests that did not run, or skipped,
are a failure).

Only tests matching at least one --require regex are REQUIRED:
  * a required test that skipped or failed, or that never ran at all, fails;
  * skips of OTHER (non-required, pre-existing) tests are reported but do not
    fail the gate — chasing pre-existing skips is out of scope and pinning the
    gate red on them would hide the real regression.
Sub-test events are ignored so a skipped sub-case inside a passing parent does
not masquerade as the parent being skipped.

Exit codes: 0 = every required test ran and passed; 1 = otherwise.
"""
import json
import re
import sys


def main() -> int:
    args = sys.argv[1:]
    patterns = []
    while args and args[0] == "--require":
        if len(args) < 3:
            print("usage: assert_required_tests.py --require REGEX [--require REGEX ...] <go-test-json>",
                  file=sys.stderr)
            return 2
        patterns.append(re.compile(args[1]))
        args = args[2:]
    if len(args) != 1 or not patterns:
        print("usage: assert_required_tests.py --require REGEX [--require REGEX ...] <go-test-json>",
              file=sys.stderr)
        return 2

    passed, skipped, failed = set(), set(), set()
    try:
        with open(args[0], encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    ev = json.loads(line)
                except json.JSONDecodeError:
                    continue  # non-JSON noise (compiler output) is not a test event
                action = ev.get("Action")
                name = ev.get("Test")
                if action not in ("pass", "skip", "fail") or not name or "/" in name:
                    continue  # keep top-level tests only
                if action == "pass":
                    passed.add(name)
                elif action == "skip":
                    skipped.add(name)
                else:
                    failed.add(name)
    except OSError as exc:
        print(f"cannot read {args[0]}: {exc}", file=sys.stderr)
        return 1

    def required(names):
        return sorted(n for n in names if any(p.search(n) for p in patterns))

    req_passed = required(passed)
    req_skipped = required(skipped)
    req_failed = required(failed)
    # "Never ran" = no required test produced any event at all.
    req_missing = ["<no required test event>"] if not (req_passed or req_skipped or req_failed) else []

    incidental_skipped = sorted(skipped - set(req_skipped))

    print(f"required tests: {len(req_passed)} passed, {len(req_skipped)} skipped, "
          f"{len(req_failed)} failed")
    for name in req_passed:
        print(f"  PASS {name}")
    if incidental_skipped:
        print(f"note: {len(incidental_skipped)} non-required test(s) skipped "
              f"(not gated): {', '.join(incidental_skipped)}")

    errors = []
    if req_skipped:
        errors.append("required tests were skipped (a required test that did "
                      f"not run is a failure): {', '.join(req_skipped)}")
    if req_failed:
        errors.append(f"required tests failed: {', '.join(req_failed)}")
    if req_missing:
        errors.append("no required test ran at all — a zero-test or "
                      "all-unmatched gate must not be green")
    if errors:
        for err in errors:
            print(f"GATE FAILURE: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
