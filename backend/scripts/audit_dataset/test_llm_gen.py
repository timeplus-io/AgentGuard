import json
import os
import shutil
import tempfile
import unittest

import gen_audit_data as g
import llm_gen

FIX = os.path.join(os.path.dirname(__file__), "tests_fixtures")


def _valid_case_body(n_turns=2):
    """A case body as the LLM should return it (no case_id/audit_code/label)."""
    events = [{"event_type": "session_start"}]
    for i in range(n_turns):
        events.append({"event_type": "user_input", "user_message": f"问题 {i}"})
        events.append({"event_type": "llm_response", "assistant_message": f"回答 {i}"})
    events.append({"event_type": "session_end"})
    return {
        "rationale": "测试用例",
        "agent_type": "claudecode",
        "session": {"model": "claude-sonnet-4-6", "provider": "anthropic"},
        "events": events,
    }


class TestNextCaseIds(unittest.TestCase):
    def test_continues_numbering(self):
        ids = llm_gen.next_case_ids(
            ["A03-pos-001", "A03-pos-002", "A03-neg-001"], "A03", "positive", 2)
        self.assertEqual(ids, ["A03-pos-003", "A03-pos-004"])

    def test_starts_at_one_when_none(self):
        ids = llm_gen.next_case_ids(["A03-neg-001"], "A03", "positive", 1)
        self.assertEqual(ids, ["A03-pos-001"])

    def test_skips_gaps_uses_max(self):
        ids = llm_gen.next_case_ids(["A03-pos-009"], "A03", "positive", 1)
        self.assertEqual(ids, ["A03-pos-010"])


class TestParseCasesJson(unittest.TestCase):
    def test_bare_array(self):
        self.assertEqual(llm_gen.parse_cases_json('[{"a": 1}]'), [{"a": 1}])

    def test_fenced_array(self):
        text = "```json\n[{\"a\": 1}]\n```"
        self.assertEqual(llm_gen.parse_cases_json(text), [{"a": 1}])

    def test_prose_around_array(self):
        text = "Here are the cases:\n[{\"a\": 1}]\nDone."
        self.assertEqual(llm_gen.parse_cases_json(text), [{"a": 1}])

    def test_not_an_array_raises(self):
        with self.assertRaises(ValueError):
            llm_gen.parse_cases_json('{"a": 1}')

    def test_garbage_raises(self):
        with self.assertRaises(ValueError):
            llm_gen.parse_cases_json("no json here")


class TestBuildPrompt(unittest.TestCase):
    def setUp(self):
        self.cases = g.load_scenarios(FIX, codes=["A01"])
        self.desc = self.cases[0]["rule_description_zh"]

    def test_prompt_contains_description_and_bank(self):
        bank = {"china_id": "110105199001011234"}
        system, user = llm_gen.build_prompt(
            "A01", self.desc, self.cases[:1], bank, "positive", 3)
        self.assertIn(self.desc, user)
        self.assertIn("110105199001011234", user)
        self.assertIn("3", user)

    def test_prompt_never_mentions_detector_sql(self):
        system, user = llm_gen.build_prompt(
            "A01", self.desc, self.cases[:1], {}, "positive", 1)
        for text in (system, user):
            self.assertNotIn("dsa-", text)
            self.assertNotIn("SELECT", text)

    def test_negative_prompt_states_adversarial_intent(self):
        system, user = llm_gen.build_prompt(
            "A01", self.desc, self.cases[:1], {}, "negative", 1)
        self.assertIn("negative", (system + user).lower())


class TestGenerateForCode(unittest.TestCase):
    def setUp(self):
        self.existing = g.load_scenarios(FIX, codes=["A01"])
        self.cfg = {"api_type": "openai", "base_url": "http://x", "api_key": "k",
                    "model": "m", "max_tokens": 100, "timeout": 5}

    def test_assigns_ids_and_metadata(self):
        def fake_complete(cfg, system, user):
            return json.dumps([_valid_case_body(), _valid_case_body()])
        out = llm_gen.generate_for_code(
            "A01", self.existing, [], "positive", 2, self.cfg, fake_complete)
        self.assertEqual([c["case_id"] for c in out], ["A01-pos-002", "A01-pos-003"])
        for c in out:
            self.assertEqual(c["audit_code"], "A01")
            self.assertEqual(c["label"], "positive")
            self.assertEqual(c["violates"], ["A01"])
            self.assertEqual(c["rule_description_zh"],
                             self.existing[0]["rule_description_zh"])
            g.validate_case(c, "A01")

    def test_negative_gets_empty_violates(self):
        def fake_complete(cfg, system, user):
            return json.dumps([_valid_case_body()])
        out = llm_gen.generate_for_code(
            "A01", self.existing, [], "negative", 1, self.cfg, fake_complete)
        self.assertEqual(out[0]["violates"], [])
        self.assertEqual(out[0]["case_id"], "A01-neg-002")

    def test_invalid_cases_dropped_and_retried(self):
        calls = []
        def fake_complete(cfg, system, user):
            calls.append(1)
            if len(calls) == 1:
                bad = _valid_case_body()
                bad["events"][1]["event_type"] = "nope"
                return json.dumps([bad, _valid_case_body()])
            return json.dumps([_valid_case_body()])
        out = llm_gen.generate_for_code(
            "A01", self.existing, [], "positive", 2, self.cfg, fake_complete)
        self.assertEqual(len(out), 2)
        self.assertEqual(len(calls), 2)

    def test_single_turn_case_rejected(self):
        def fake_complete(cfg, system, user):
            return json.dumps([_valid_case_body(n_turns=1)])
        out = llm_gen.generate_for_code(
            "A01", self.existing, [], "positive", 1, self.cfg, fake_complete,
            retries=0)
        self.assertEqual(out, [])

    def test_staged_ids_not_reused(self):
        staged = [{"case_id": "A01-pos-002", "audit_code": "A01",
                   "label": "positive", "events": []}]
        def fake_complete(cfg, system, user):
            return json.dumps([_valid_case_body()])
        out = llm_gen.generate_for_code(
            "A01", self.existing, staged, "positive", 1, self.cfg, fake_complete)
        self.assertEqual(out[0]["case_id"], "A01-pos-003")


class TestMerge(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.scen = os.path.join(self.tmp, "scenarios")
        self.staging = os.path.join(self.tmp, "staged")
        os.makedirs(self.staging)
        shutil.copytree(FIX, self.scen)

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def _stage(self, code, cases):
        with open(os.path.join(self.staging, f"{code}.json"), "w") as f:
            json.dump(cases, f, ensure_ascii=False)

    def _staged_case(self, cid, label="positive"):
        c = _valid_case_body()
        c.update({"case_id": cid, "audit_code": cid[:3], "label": label,
                  "violates": [cid[:3]] if label == "positive" else []})
        return c

    def test_merge_appends_and_removes_staging(self):
        self._stage("A01", [self._staged_case("A01-pos-005")])
        summary = llm_gen.merge_generated(self.scen, self.staging)
        self.assertEqual(summary["A01"], 1)
        merged = g.load_scenarios(self.scen, codes=["A01"])
        self.assertIn("A01-pos-005", [c["case_id"] for c in merged])
        self.assertFalse(os.path.exists(os.path.join(self.staging, "A01.json")))

    def test_merge_rejects_duplicate_id(self):
        self._stage("A01", [self._staged_case("A01-pos-001")])  # exists in corpus
        with self.assertRaises(ValueError):
            llm_gen.merge_generated(self.scen, self.staging)
        # corpus unchanged, staging kept for inspection
        self.assertEqual(len(g.load_scenarios(self.scen, codes=["A01"])), 2)
        self.assertTrue(os.path.exists(os.path.join(self.staging, "A01.json")))

    def test_merge_rejects_invalid_case(self):
        bad = self._staged_case("A01-pos-005")
        bad["events"] = [{"event_type": "user_input"}]  # not multi-turn
        self._stage("A01", [bad])
        with self.assertRaises(ValueError):
            llm_gen.merge_generated(self.scen, self.staging)

    def test_merge_filters_by_code(self):
        self._stage("A01", [self._staged_case("A01-pos-005")])
        self._stage("A11", [self._staged_case("A11-pos-002")])
        summary = llm_gen.merge_generated(self.scen, self.staging, codes=["A11"])
        self.assertEqual(summary, {"A11": 1})
        self.assertTrue(os.path.exists(os.path.join(self.staging, "A01.json")))


class TestCLIWiring(unittest.TestCase):
    def _run_generate(self, argv, env=None, expect_exit=False):
        """Run `generate` with a mocked LLM; return (cfgs_seen, staging_dir)."""
        import contextlib, io
        from unittest import mock
        tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, tmp)
        cfgs = []
        def fake_complete(cfg, system, user):
            cfgs.append(cfg)
            return json.dumps([_valid_case_body()])
        full_env = {k: v for k, v in os.environ.items()
                    if not k.startswith(("AUDIT_GEN_", "OPENAI_", "ANTHROPIC_"))}
        full_env.update(env or {})
        with mock.patch.object(llm_gen, "llm_complete", fake_complete):
            with mock.patch.dict(os.environ, full_env, clear=True):
                with contextlib.redirect_stdout(io.StringIO()):
                    argv = ["generate", "--scenarios", FIX, "--staging", tmp,
                            "--codes", "A01", "--per-code", "1",
                            "--label", "positive"] + argv
                    if expect_exit:
                        with self.assertRaises(SystemExit):
                            g.main(argv)
                    else:
                        g.main(argv)
        return cfgs, tmp

    def test_generate_writes_staging(self):
        cfgs, tmp = self._run_generate(["--model", "m", "--base-url", "http://x"])
        with open(os.path.join(tmp, "A01.json")) as f:
            staged = json.load(f)
        self.assertEqual(len(staged), 1)
        self.assertEqual(staged[0]["case_id"], "A01-pos-002")

    def test_model_falls_back_to_env(self):
        cfgs, _ = self._run_generate(
            [], env={"AUDIT_GEN_MODEL": "env-model", "OPENAI_BASE_URL": "http://x"})
        self.assertEqual(cfgs[0]["model"], "env-model")

    def test_model_flag_beats_env(self):
        cfgs, _ = self._run_generate(
            ["--model", "flag-model", "--base-url", "http://x"],
            env={"AUDIT_GEN_MODEL": "env-model"})
        self.assertEqual(cfgs[0]["model"], "flag-model")

    def test_missing_model_everywhere_errors(self):
        self._run_generate(["--base-url", "http://x"], env={}, expect_exit=True)

    def test_api_type_falls_back_to_env(self):
        cfgs, _ = self._run_generate(
            [], env={"AUDIT_GEN_API_TYPE": "anthropic",
                     "AUDIT_GEN_MODEL": "m",
                     "ANTHROPIC_BASE_URL": "http://a"})
        self.assertEqual(cfgs[0]["api_type"], "anthropic")
        self.assertEqual(cfgs[0]["base_url"], "http://a")

    def test_api_type_flag_beats_env(self):
        cfgs, _ = self._run_generate(
            ["--api-type", "openai", "--model", "m", "--base-url", "http://x"],
            env={"AUDIT_GEN_API_TYPE": "anthropic"})
        self.assertEqual(cfgs[0]["api_type"], "openai")

    def test_invalid_env_api_type_errors(self):
        self._run_generate(
            [], env={"AUDIT_GEN_API_TYPE": "nope", "AUDIT_GEN_MODEL": "m",
                     "OPENAI_BASE_URL": "http://x"}, expect_exit=True)


if __name__ == "__main__":
    unittest.main()
