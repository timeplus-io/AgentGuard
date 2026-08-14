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


if __name__ == "__main__":
    unittest.main()
