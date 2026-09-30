#!/usr/bin/env python3
"""Tests for the CodeQL result scope."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("scope_results", Path(__file__).with_name("scope-results.py"))
scope = importlib.util.module_from_spec(spec)
spec.loader.exec_module(scope)

COSMOS = "crypto-com/cosmos-sdk-codeql/map-iteration"
STANDARD = "go/index-out-of-bounds"


class ScopeTest(unittest.TestCase):
    def test_state_machine_keeps_every_rule(self):
        for path in ("x/reserve/keeper/recognition.go", "app/mempool/lanes.go", "abci/oracle/oracle_votes.go",
                     "pkg/chain/coins.go", "pkg/decimal/decimal.go"):
            for rule in (COSMOS, STANDARD):
                self.assertIsNone(scope.out_of_scope(rule, path), (rule, path))

    def test_off_chain_drops_only_cosmos_rules(self):
        for path in ("pricefeed/sidecar/runtime/prices.go", "cmd/arkd/cmd/vote_extensions.go",
                     "tests/e2e/chainsuite/chain.go", "abci/oracle/metrics/metrics.go", "pkg/metrics/metrics.go",
                     "pkg/tlsconfig/material.go"):
            self.assertEqual(scope.out_of_scope(COSMOS, path), "off-chain", path)
            self.assertIsNone(scope.out_of_scope(STANDARD, path), path)

    def test_generated_drops_every_rule(self):
        for path in ("api/ark/oracle/v1/tx.pulsar.go", "x/oracle/types/tx.pb.go", "x/oracle/types/query.pb.gw.go",
                     "abci/voteextension/types/vote_extension.pb.go"):
            for rule in (COSMOS, STANDARD):
                self.assertEqual(scope.out_of_scope(rule, path), "generated", (rule, path))

    def test_scope_rewrites_runs(self):
        def result(rule, path):
            return {"ruleId": rule, "locations": [{"physicalLocation": {"artifactLocation": {"uri": path}}}]}

        sarif = {"runs": [{"results": [result(COSMOS, "x/a.go"), result(COSMOS, "pricefeed/a.go"),
                                       result(STANDARD, "api/a.go"), {"ruleId": STANDARD}]}]}
        dropped = scope.scope(sarif)
        self.assertEqual([r.get("locations") and r["locations"][0]["physicalLocation"]["artifactLocation"]["uri"]
                          for r in sarif["runs"][0]["results"]], ["x/a.go", None])
        self.assertEqual(dropped, {("off-chain", COSMOS): 1, ("generated", STANDARD): 1})


if __name__ == "__main__":
    unittest.main()
