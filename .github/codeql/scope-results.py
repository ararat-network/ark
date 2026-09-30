#!/usr/bin/env python3
"""Scope CodeQL results in place before upload. See tests/README.md, "Static analysis boundaries"."""
from collections import Counter
from fnmatch import fnmatchcase
import json
import sys

# Generated code stays in the database for dataflow; its findings belong to the generator.
GENERATED = ("api/*", "*.pb.go", "*.pb.gw.go", "*.pulsar.go")

# The Cosmos SDK checks (map order, wall clock, floats, goroutines) guard agreement between nodes,
# so they apply only where code runs in the state machine.
COSMOS_RULES = "crypto-com/cosmos-sdk-codeql/"
OFF_CHAIN = (
    "cmd/*",
    "pricefeed/*",
    "tests/*",
    "*/metrics/*",
    "pkg/fsutil/*",
    "pkg/grpcconn/*",
    "pkg/telemetry/*",
    "pkg/tlsconfig/*",
)


def out_of_scope(rule, path):
    if any(fnmatchcase(path, pattern) for pattern in GENERATED):
        return "generated"
    if rule.startswith(COSMOS_RULES) and any(fnmatchcase(path, pattern) for pattern in OFF_CHAIN):
        return "off-chain"
    return None


def scope(sarif):
    dropped = Counter()
    for run in sarif["runs"]:
        kept = []
        for result in run.get("results", []):
            rule = result.get("ruleId", "")
            locations = result.get("locations") or [{}]
            path = locations[0].get("physicalLocation", {}).get("artifactLocation", {}).get("uri", "")
            reason = out_of_scope(rule, path)
            if reason:
                dropped[reason, rule] += 1
            else:
                kept.append(result)
        run["results"] = kept
    return dropped


def main(paths):
    for path in paths:
        with open(path) as stream:
            sarif = json.load(stream)
        dropped = scope(sarif)
        with open(path, "w") as stream:
            json.dump(sarif, stream)
        kept = sum(len(run["results"]) for run in sarif["runs"])
        print(f"{path}: kept {kept} results, dropped {sum(dropped.values())}")
        for (reason, rule), count in sorted(dropped.items()):
            print(f"  {count:4d} {reason:9s} {rule}")


if __name__ == "__main__":
    main(sys.argv[1:])
