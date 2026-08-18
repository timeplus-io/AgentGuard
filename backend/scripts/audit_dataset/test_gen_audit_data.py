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


class TestFilterCases(unittest.TestCase):
    def _cases(self):
        return g.load_scenarios(FIX)

    def test_selects_only_named_case(self):
        picked = g.filter_cases(self._cases(), ["A01-pos-001"])
        self.assertEqual([c["case_id"] for c in picked], ["A01-pos-001"])

    def test_selects_multiple_cases(self):
        picked = g.filter_cases(self._cases(), ["A01-neg-001", "A11-pos-001"])
        self.assertEqual(sorted(c["case_id"] for c in picked),
                         ["A01-neg-001", "A11-pos-001"])

    def test_case_insensitive_match(self):
        picked = g.filter_cases(self._cases(), ["a01-POS-001"])
        self.assertEqual([c["case_id"] for c in picked], ["A01-pos-001"])

    def test_unknown_id_raises_with_id_in_message(self):
        with self.assertRaises(ValueError) as ctx:
            g.filter_cases(self._cases(), ["A01-pos-999"])
        self.assertIn("A01-pos-999", str(ctx.exception))


class TestCasesFlag(unittest.TestCase):
    def test_load_dry_run_with_cases_flag(self):
        import contextlib
        import io
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            g.main(["load", "--scenarios", FIX, "--cases", "A01-pos-001",
                    "--dry-run"])
        self.assertIn("1 cases", buf.getvalue())


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


class TestPayload(unittest.TestCase):
    def _cases(self):
        return g.load_scenarios(FIX)

    def test_jsoneachrow_one_line_per_row(self):
        now = dt.datetime(2026, 8, 14, 12, 0, 0, tzinfo=dt.timezone.utc)
        rows = g.case_to_rows(self._cases()[0], now, "audit-demo")
        body = g.rows_to_jsoneachrow(rows)
        self.assertEqual(len(body.splitlines()), len(rows))
        for line in body.splitlines():
            obj = _json.loads(line)
            self.assertEqual(set(obj.keys()), set(g.CIM_COLUMNS))

    def test_insert_payload_prefix(self):
        now = dt.datetime(2026, 8, 14, 12, 0, 0, tzinfo=dt.timezone.utc)
        rows = g.case_to_rows(self._cases()[0], now, "audit-demo")
        payload = g.build_insert_payload(rows)
        self.assertTrue(payload.startswith(
            b"INSERT INTO agentguard_cim_event FORMAT JSONEachRow\n"))

    def test_build_all_rows_staggers_cases(self):
        now = dt.datetime(2026, 8, 14, 12, 0, 0, tzinfo=dt.timezone.utc)
        cases = self._cases()
        rows = g.build_all_rows(cases, now, "audit-demo", repeat=1)
        # every case represented
        sids = set(r["session_id"] for r in rows)
        self.assertEqual(len(sids), len(cases))

    def test_repeat_creates_distinct_sessions(self):
        now = dt.datetime(2026, 8, 14, 12, 0, 0, tzinfo=dt.timezone.utc)
        cases = [self._cases()[0]]
        rows = g.build_all_rows(cases, now, "audit-demo", repeat=2)
        sids = set(r["session_id"] for r in rows)
        self.assertEqual(len(sids), 2)


class TestScoring(unittest.TestCase):
    def test_expected_rule_id(self):
        self.assertEqual(g.expected_rule_id("A01"), "dsa-a01")
        self.assertIsNone(g.expected_rule_id("A02"))

    def test_confusion_matrix(self):
        cases = [
            {"case_id": "A01-pos-001", "audit_code": "A01", "label": "positive", "events": []},
            {"case_id": "A01-pos-002", "audit_code": "A01", "label": "positive", "events": []},
            {"case_id": "A01-neg-001", "audit_code": "A01", "label": "negative", "events": []},
            {"case_id": "A01-neg-002", "audit_code": "A01", "label": "negative", "events": []},
        ]
        fired = {
            ("dsa-a01", "audit-A01-pos-001"),   # TP
            ("dsa-a01", "audit-A01-neg-001"),   # FP
            # pos-002 not fired -> FN ; neg-002 not fired -> TN
        }
        rep = g.score_cases(cases, fired)["A01"]
        self.assertEqual((rep["tp"], rep["fp"], rep["fn"], rep["tn"]), (1, 1, 1, 1))
        self.assertAlmostEqual(rep["precision"], 0.5)
        self.assertAlmostEqual(rep["recall"], 0.5)
        self.assertEqual(rep["misses"], ["A01-pos-002"])
        self.assertEqual(rep["false_alarms"], ["A01-neg-001"])

    def test_no_rule_code(self):
        cases = [{"case_id": "A02-pos-001", "audit_code": "A02", "label": "positive", "events": []}]
        rep = g.score_cases(cases, set())["A02"]
        self.assertTrue(rep["no_rule"])
        self.assertEqual(rep["fn"], 1)  # positive undetected (known gap)

    def test_cross_fire_ignored_for_other_code(self):
        # dsa-a06 firing on an A06 session must not affect A01's matrix
        cases = [{"case_id": "A01-neg-001", "audit_code": "A01", "label": "negative", "events": []}]
        fired = {("dsa-a06", "audit-A01-neg-001")}
        rep = g.score_cases(cases, fired)["A01"]
        self.assertEqual((rep["fp"], rep["tn"]), (0, 1))


class TestQueryParse(unittest.TestCase):
    def test_parse_tsv(self):
        text = "dsa-a01\taudit-A01-pos-001\ndsa-a06\taudit-A06-pos-002\n"
        fired = g.parse_security_events_tsv(text)
        self.assertIn(("dsa-a01", "audit-A01-pos-001"), fired)
        self.assertIn(("dsa-a06", "audit-A06-pos-002"), fired)

    def test_parse_tsv_ignores_blank_lines(self):
        self.assertEqual(g.parse_security_events_tsv("\n\n"), set())


if __name__ == "__main__":
    unittest.main()
