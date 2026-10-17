#!/usr/bin/env python3
"""Fail closed on the original Go exit status and an exact test manifest."""
import argparse
import json
from pathlib import Path
import sys


def validate(log_path, required, exit_code):
    errors = []
    if exit_code != 0:
        errors.append(f"go test exited {exit_code}")
    expected = {(r["package"], r["test"]) for r in required}
    if not expected or len(expected) != len(required):
        errors.append("required suite is empty or contains duplicate identities")
    started, finished, ran, passed, terminal_tests = set(), {}, set(), set(), set()
    events = 0
    try:
        with open(log_path, encoding="utf-8") as stream:
            for number, line in enumerate(stream, 1):
                if not line.strip():
                    continue
                try:
                    ev = json.loads(line)
                except (ValueError, TypeError):
                    errors.append(f"invalid or truncated JSON at line {number}")
                    continue
                if not isinstance(ev, dict) or not isinstance(ev.get("Action"), str):
                    errors.append(f"invalid Go event at line {number}")
                    continue
                events += 1
                action, pkg, test = ev["Action"], ev.get("Package"), ev.get("Test")
                if action not in {"start", "run", "pause", "cont", "pass", "fail", "skip", "output", "bench", "build-output", "build-fail"}:
                    errors.append(f"unknown Go event action at line {number}: {action}")
                if action == "build-fail":
                    errors.append(f"build failed: {ev.get('ImportPath', pkg)}")
                if not pkg:
                    if action not in ("build-output", "build-fail"):
                        errors.append(f"event without Package at line {number}")
                    continue
                if pkg in finished:
                    errors.append(f"event after package completion at line {number}: {pkg}")
                if action == "start":
                    if pkg in started:
                        errors.append(f"duplicate package run: {pkg}")
                    started.add(pkg)
                if test:
                    identity = (pkg, test)
                    if action == "run":
                        ran.add(identity)
                    if action == "pass":
                        passed.add(identity)
                    if action in ("pass", "fail", "skip"):
                        if identity in terminal_tests:
                            errors.append(f"duplicate test completion: {pkg}:{test}")
                        terminal_tests.add(identity)
                    if action == "fail":
                        errors.append(f"test failed: {pkg}:{test}")
                    if action == "skip" and any(pkg == p and (test == t or test.startswith(t + "/")) for p, t in expected):
                        errors.append(f"required test/subtest skipped: {pkg}:{test}")
                elif action in ("pass", "fail", "skip"):
                    if pkg in finished:
                        errors.append(f"duplicate package completion: {pkg}")
                    finished[pkg] = action
                    if action == "fail":
                        errors.append(f"package failed: {pkg}")
    except OSError as exc:
        errors.append(f"cannot read test log: {exc}")
    if not events:
        errors.append("empty Go event stream")
    for pkg in sorted(started | set(finished) | {p for p, _ in expected}):
        if pkg not in started or finished.get(pkg) != "pass":
            errors.append(f"package did not start and complete successfully: {pkg}")
    for pkg, test in sorted(ran - terminal_tests):
        errors.append(f"test did not complete: {pkg}:{test}")
    for identity in sorted(expected):
        if identity not in ran or identity not in passed:
            errors.append(f"required test did not run and pass: {identity[0]}:{identity[1]}")
    return errors, sorted(expected & ran & passed)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("required-tests.json"))
    parser.add_argument("--suite", required=True)
    parser.add_argument("--exit-code", required=True, type=int)
    parser.add_argument("log", type=Path)
    args = parser.parse_args()
    try:
        document = json.loads(args.manifest.read_text())
        required = document["suites"][args.suite]
        if not isinstance(required, list) or any(not isinstance(r, dict) or not isinstance(r.get("package"), str) or not isinstance(r.get("test"), str) or not r["package"] or not r["test"] for r in required):
            raise ValueError("suite must contain package and full test IDs")
    except (OSError, ValueError, KeyError, TypeError) as exc:
        print(f"GATE FAILURE: invalid required manifest/suite: {exc}", file=sys.stderr)
        return 1
    errors, passed = validate(args.log, required, args.exit_code)
    print(f"required tests: {len(passed)} passed; original exit={args.exit_code}; suite={args.suite}")
    for package, test in passed:
        print(f"  PASS {package}:{test}")
    for error in errors:
        print(f"GATE FAILURE: {error}", file=sys.stderr)
    return int(bool(errors))


if __name__ == "__main__":
    sys.exit(main())
