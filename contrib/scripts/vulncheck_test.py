#!/usr/bin/env python3
"""Tests for the govulncheck review gate, without running govulncheck."""
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("vulncheck", Path(__file__).with_name("vulncheck.py"))
vulncheck = importlib.util.module_from_spec(spec)
spec.loader.exec_module(vulncheck)


def finding(osv, fixed="", function=None):
    frame = {"module": "example.com/m", "version": "v1.0.0"}
    if function:
        frame.update(package="example.com/m/p", function=function)
    return {"finding": {"osv": osv, "fixed_version": fixed, "trace": [frame]}}


class VulncheckTest(unittest.TestCase):
    def test_parse_reads_a_pretty_printed_stream(self):
        text = "\n".join(json.dumps(m, indent=2) for m in ({"config": {}}, finding("GO-1"))) + "\n"
        self.assertEqual([next(iter(m)) for m in vulncheck.parse(text)], ["config", "finding"])

    def test_called_keeps_only_symbol_level_findings(self):
        messages = [{"osv": {"id": "GO-1", "summary": "one"}}, finding("GO-1", function="F"),
                    finding("GO-2"), finding("GO-3", fixed="v1.2.0", function="G")]
        found, summaries = vulncheck.called(messages)
        self.assertEqual(found, {"GO-1": "", "GO-3": "v1.2.0"})
        self.assertEqual(summaries["GO-1"], "one")

    def test_review(self):
        accepted = {"GO-1": "reason", "GO-2": "reason", "GO-4": "reason"}
        problems = dict(vulncheck.review({"GO-1": "", "GO-2": "v2.0.0", "GO-3": ""}, accepted))
        self.assertNotIn("GO-1", problems)
        self.assertIn("fixed in v2.0.0", problems["GO-2"])
        self.assertEqual(problems["GO-3"], "not reviewed")
        self.assertIn("no longer reported", problems["GO-4"])
        self.assertEqual(vulncheck.review({"GO-1": ""}, {"GO-1": "reason"}), [])

    def test_accepted_entries_give_reasons(self):
        entries = json.loads(vulncheck.ACCEPTED.read_text())
        self.assertEqual(len({e["id"] for e in entries}), len(entries))
        for entry in entries:
            self.assertTrue(entry["id"].startswith("GO-") and entry["reason"].strip(), entry)


if __name__ == "__main__":
    unittest.main()
