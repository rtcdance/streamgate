#!/usr/bin/env python3
"""Poll a private HLS segment URL until transfer revocation or capability expiry."""
import argparse
import time
import urllib.error
import urllib.request
from pathlib import Path


def poll_revocation(url, deadline, start_monotonic_ns=0, *, opener=None, clock=None, sleeper=None):
    if deadline <= 0 or deadline > 70:
        raise ValueError("revocation deadline must be in (0, 70] seconds")
    opener = opener or urllib.request.urlopen
    clock = clock or time.monotonic
    sleeper = sleeper or time.sleep
    start = start_monotonic_ns / 1_000_000_000 if start_monotonic_ns else clock()
    now = clock()
    if start > now:
        raise ValueError("revocation start timestamp is in the future")
    end = start + deadline
    while True:
        code = 0
        try:
            request = urllib.request.Request(url, headers={"Range": "bytes=0-0"}, method="GET")
            with opener(request, timeout=8) as response:
                code = response.status
                while response.read(65536):
                    pass
        except urllib.error.HTTPError as exc:
            code = exc.code
            exc.close()
        except (TimeoutError, OSError):
            code = 0
        elapsed = clock() - start
        if code in (401, 403):
            if elapsed > deadline:
                raise ValueError("capability revocation exceeded the configured deadline")
            print(f"M3_HTTP_TRANSFER_70S_PASS elapsed={elapsed:.3f}s")
            return 0
        if clock() >= end:
            raise ValueError(f"capability remained accepted or unverifiable for {deadline:.0f}s (last HTTP {code})")
        sleeper(0.25)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("private_url_file", type=Path)
    parser.add_argument("--deadline", type=float, default=70.0)
    parser.add_argument("--start-monotonic-ns", type=int, default=0)
    args = parser.parse_args()
    mode = args.private_url_file.stat().st_mode & 0o777
    if mode != 0o600:
        raise SystemExit("private capability URL file permissions must be 0600")
    url = args.private_url_file.read_text().strip()
    args.private_url_file.unlink()
    if not url or "playback_token=" not in url:
        raise SystemExit("private capability URL is missing its bearer token")
    try:
        return poll_revocation(url, args.deadline, args.start_monotonic_ns)
    except ValueError as exc:
        raise SystemExit(str(exc)) from exc


if __name__ == "__main__":
    raise SystemExit(main())
