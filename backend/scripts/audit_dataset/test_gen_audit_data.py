import datetime as dt
import json as _json
import os
import unittest

import gen_audit_data as g

FIX = os.path.join(os.path.dirname(__file__), "tests_fixtures")


class TestLoader(unittest.TestCase):
    def test_loads_all_fixture_cases(self):
        cases = g.load_scenarios(FIX)
        ids = sorted(c["case_id"] for c in cases)
        self.assertIn("A01-pos-001", ids)
        self.assertIn("A11-pos-001", ids)

    def test_filter_by_code(self):
        cases = g.load_scenarios(FIX, codes=["A11"])
        self.assertTrue(all(c["audit_code"] == "A11" for c in cases))
        self.assertTrue(cases)

    def test_rejects_missing_label(self):
        with self.assertRaises(ValueError):
            g.validate_case({"case_id": "A01-x", "audit_code": "A01",
                             "events": [{"event_type": "user_input"}]}, "A01")

    def test_rejects_bad_event_type(self):
        with self.assertRaises(ValueError):
            g.validate_case({"case_id": "A01-x", "audit_code": "A01",
                             "label": "positive",
                             "events": [{"event_type": "nope"}]}, "A01")

    def test_rejects_authored_session_id(self):
        with self.assertRaises(ValueError):
            g.validate_case({"case_id": "A01-x", "audit_code": "A01",
                             "label": "positive",
                             "events": [{"event_type": "user_input", "session_id": "x"}]}, "A01")

    def test_rejects_code_mismatch_with_filename(self):
        with self.assertRaises(ValueError):
            g.validate_case({"case_id": "A03-x", "audit_code": "A03",
                             "label": "positive",
                             "events": [{"event_type": "user_input"}]}, "A01")


class TestMapping(unittest.TestCase):
    def setUp(self):
        self.now = dt.datetime(2026, 8, 14, 12, 0, 0, tzinfo=dt.timezone.utc)

    def _case(self, cid):
        return next(c for c in g.load_scenarios(FIX) if c["case_id"] == cid)

    def test_all_columns_present(self):
        rows = g.case_to_rows(self._case("A01-pos-001"), self.now, "audit-demo")
        for row in rows:
            self.assertEqual(set(row.keys()), set(g.CIM_COLUMNS))

    def test_session_id_from_case(self):
        rows = g.case_to_rows(self._case("A01-pos-001"), self.now, "audit-demo")
        self.assertTrue(all(r["session_id"] == "audit-A01-pos-001" for r in rows))

    def test_events_are_time_ordered_ending_at_now(self):
        rows = g.case_to_rows(self._case("A01-pos-001"), self.now, "audit-demo")
        times = [r["event_time"] for r in rows]
        self.assertEqual(times, sorted(times))
        # last event stamped at `now`
        self.assertEqual(rows[-1]["event_time"], "2026-08-14 12:00:00.000")

    def test_field_overrides_and_hook_names(self):
        rows = g.case_to_rows(self._case("A01-pos-001"), self.now, "audit-demo")
        ui = next(r for r in rows if r["event_type"] == "user_input")
        self.assertEqual(ui["raw_hook_name"], "message_received")  # openclaw
        self.assertEqual(ui["model"], "claude-sonnet-4-6")
        self.assertNotEqual(ui["user_message"], "")
        self.assertTrue(_json.loads(ui["raw_event_data"]))  # non-empty JSON

    def test_a11_empty_agent_id(self):
        rows = g.case_to_rows(self._case("A11-pos-001"), self.now, "audit-demo")
        self.assertTrue(all(r["agent_id"] == "" for r in rows))
        self.assertTrue(all(r["session_id"] == "audit-A11-pos-001" for r in rows))

    def test_deployment_id_applied(self):
        rows = g.case_to_rows(self._case("A01-pos-001"), self.now, "audit-demo")
        self.assertTrue(all(r["deployment_id"] == "audit-demo" for r in rows))


if __name__ == "__main__":
    unittest.main()
