#!/usr/bin/env python3
"""Run govulncheck and fail on reachable findings that have not been reviewed.

vulncheck-accepted.json records why each accepted finding does not affect Ark. An accepted finding
fails again once govulncheck reports a fix, and an entry fails once its finding is gone, so the
list holds only live, unfixable, reviewed findings.
"""
import json
from pathlib import Path
import subprocess
import sys

ACCEPTED = Path(__file__).with_name("vulncheck-accepted.json")


def parse(text):
    """Yield the objects of govulncheck's JSON stream."""
    decoder = json.JSONDecoder()
    index = 0
    while True:
        while index < len(text) and text[index].isspace():
            index += 1
        if index == len(text):
            return
        value, index = decoder.raw_decode(text, index)
        yield value


def called(messages):
    """Return {id: fixed version or ""} for vulnerable symbols the code calls, and advisory summaries."""
    found, summaries = {}, {}
    for message in messages:
        if "osv" in message:
            summaries[message["osv"]["id"]] = message["osv"].get("summary", "")
        finding = message.get("finding")
        # Module- and package-level findings are vulnerable code Ark links but never calls.
        if finding and finding["trace"][0].get("function"):
            found[finding["osv"]] = finding.get("fixed_version", "")
    return found, summaries


def review(found, accepted):
    """Return (id, problem) pairs; empty when every finding is accepted and still unfixed."""
    problems = []
    for osv, fixed in sorted(found.items()):
        if osv not in accepted:
            problems.append((osv, f"not reviewed{f'; fixed in {fixed}' if fixed else ''}"))
        elif fixed:
            problems.append((osv, f"accepted, but fixed in {fixed}: upgrade and drop the entry"))
    for osv in sorted(set(accepted) - set(found)):
        problems.append((osv, "accepted, but no longer reported: drop the entry"))
    return problems


def main():
    accepted = {entry["id"]: entry["reason"] for entry in json.loads(ACCEPTED.read_text())}
    run = subprocess.run(["govulncheck", "-format", "json", "./..."], capture_output=True, text=True)
    if run.returncode:
        sys.stderr.write(run.stderr)
        return run.returncode
    found, summaries = called(parse(run.stdout))
    for osv in sorted(found):
        print(f"{osv}  {'accepted' if osv in accepted else 'NEW':8s}  {summaries.get(osv, '')}")
    problems = review(found, accepted)
    sys.stdout.flush()
    for osv, problem in problems:
        print(f"FAIL {osv}: {problem}", file=sys.stderr)
    if problems:
        print("Run govulncheck ./... for call traces.", file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
