import os
import unittest
import datetime as dt

import gen_audit_data as g

SCEN = os.path.join(os.path.dirname(__file__), "scenarios")
ALL_CODES = [f"A{n:02d}" for n in range(1, 16)]


@unittest.skipUnless(os.path.isdir(SCEN) and os.listdir(SCEN),
                     "corpus not authored yet")
class TestCorpus(unittest.TestCase):
    def test_all_15_codes_present(self):
        for code in ALL_CODES:
            self.assertTrue(os.path.exists(os.path.join(SCEN, f"{code}.json")),
                            f"missing scenarios/{code}.json")

    def test_five_pos_five_neg_per_code(self):
        for code in ALL_CODES:
            cases = g.load_scenarios(SCEN, codes=[code])
            pos = [c for c in cases if c["label"] == "positive"]
            neg = [c for c in cases if c["label"] == "negative"]
            self.assertGreaterEqual(len(pos), 5, f"{code}: <5 positives")
            self.assertGreaterEqual(len(neg), 5, f"{code}: <5 negatives")

    def test_case_ids_unique(self):
        cases = g.load_scenarios(SCEN)
        ids = [c["case_id"] for c in cases]
        self.assertEqual(len(ids), len(set(ids)))

    def test_every_case_maps_cleanly(self):
        now = dt.datetime(2026, 8, 14, 12, 0, 0, tzinfo=dt.timezone.utc)
        for c in g.load_scenarios(SCEN):
            rows = g.case_to_rows(c, now, "audit-demo")
            self.assertTrue(rows)
            for r in rows:
                self.assertEqual(set(r.keys()), set(g.CIM_COLUMNS))

    def test_multi_turn(self):
        # every case is a real conversation: >= 2 user_input turns OR
        # a tool round, not a single Q&A.
        for c in g.load_scenarios(SCEN):
            ets = [e["event_type"] for e in c["events"]]
            turns = ets.count("user_input")
            has_tool = "tool_invoke" in ets or "tool_complete" in ets
            self.assertTrue(turns >= 2 or has_tool,
                            f"{c['case_id']}: not multi-turn")
