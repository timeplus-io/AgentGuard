-- rp-013 live MV validation fixtures (six scenarios, 36 rows).
--
-- Used to confirm v0.4.0 detection behaviour against a running
-- Timeplus Enterprise instance. See README.md in this directory
-- for the full runbook (docker setup, MV install, expected counts).
--
-- Event_time order is load-bearing: streaming aggregations drop events
-- that arrive with event_time older than the current watermark. The
-- single INSERT below preserves chronological order across all six
-- scenarios so no event is treated as late.
--
-- Row-by-row scenario attribution (do NOT inline comments inside the
-- VALUES body itself — Timeplus's INSERT parser rejects '--' between
-- value tuples with Code: 27, expected '('):
--   10:00:10  s4-sustained  r4a  4000/500   sustained-#1
--   10:00:30  s3-relative   r3a   500/500   baseline-#1
--   10:01:00  s1-abs        r1-abs 10000/500 absolute spike (Branch A)
--   10:01:00  s3-relative   r3b   500/500   baseline-#2
--   10:01:10  s4-sustained  r4b  4000/500   sustained-#2
--   10:01:30  s2-codegen    r2   10000/5000 codegen suppression (must NOT fire)
--   10:01:30  s3-relative   r3c   500/500   baseline-#3
--   10:02:00  s3-relative   r3d   500/500   baseline-#4
--   10:02:10  s4-sustained  r4c  4000/500   sustained-#3
--   10:02:30  s3-relative   r3e   500/500   baseline-#5
--   10:03:00  s3-relative   r3-spike 5000/500  5x relative spike (Branch B)
--   10:04:30  s5-boundary   r5a  4000/500   boundary-split fixture #1
--   10:05:30  s5-boundary   r5b  4000/500   boundary-split fixture #2
--   10:05:30  s5-boundary   r5c  4000/500   boundary-split fixture #3
--   11:00:00  s6-drip       r6a  5000/500   drip-gap call #1 (8 min apart)
--   11:08:00  s6-drip       r6b  5000/500   drip-gap call #2
--   11:16:00  s6-drip       r6c  5000/500   drip-gap call #3
--   11:30:00  s-trailer     r-trailer 1/1   watermark trailer
--
-- Tumble vs hop reminder for the boundary scenario: under tumble(5m)
-- the s5 calls split 1/2 across [10:00-10:05) and [10:05-10:10) so
-- the sustained branch sees count=1 and count=2, never 3. Under
-- hop(slide=1m, window=5m) the windows [10:01-10:06) through
-- [10:04-10:09) each contain all three s5 calls and the sustained
-- branch fires.

INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags) VALUES
  ('2026-05-11 10:00:10', 'llm.output_tokens', 4000.0, 'claudecode', 'agent-sus', 'dep-test', 's4-sustained', {'run_id': 'r4a', 'tool_name': ''}),
  ('2026-05-11 10:00:10', 'llm.input_tokens',   500.0, 'claudecode', 'agent-sus', 'dep-test', 's4-sustained', {'run_id': 'r4a', 'tool_name': ''}),
  ('2026-05-11 10:00:30', 'llm.output_tokens',  500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3a', 'tool_name': ''}),
  ('2026-05-11 10:00:30', 'llm.input_tokens',   500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3a', 'tool_name': ''}),
  ('2026-05-11 10:01:00', 'llm.output_tokens', 10000.0, 'claudecode', 'agent-abs', 'dep-test', 's1-abs',       {'run_id': 'r1-abs',     'tool_name': ''}),
  ('2026-05-11 10:01:00', 'llm.input_tokens',   500.0, 'claudecode', 'agent-abs', 'dep-test', 's1-abs',       {'run_id': 'r1-abs',     'tool_name': ''}),
  ('2026-05-11 10:01:00', 'llm.output_tokens',  500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3b', 'tool_name': ''}),
  ('2026-05-11 10:01:00', 'llm.input_tokens',   500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3b', 'tool_name': ''}),
  ('2026-05-11 10:01:10', 'llm.output_tokens', 4000.0, 'claudecode', 'agent-sus', 'dep-test', 's4-sustained', {'run_id': 'r4b', 'tool_name': ''}),
  ('2026-05-11 10:01:10', 'llm.input_tokens',   500.0, 'claudecode', 'agent-sus', 'dep-test', 's4-sustained', {'run_id': 'r4b', 'tool_name': ''}),
  ('2026-05-11 10:01:30', 'llm.output_tokens', 10000.0, 'claudecode', 'agent-cg',  'dep-test', 's2-codegen',  {'run_id': 'r2-codegen', 'tool_name': ''}),
  ('2026-05-11 10:01:30', 'llm.input_tokens',  5000.0, 'claudecode', 'agent-cg',  'dep-test', 's2-codegen',   {'run_id': 'r2-codegen', 'tool_name': ''}),
  ('2026-05-11 10:01:30', 'llm.output_tokens',  500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3c', 'tool_name': ''}),
  ('2026-05-11 10:01:30', 'llm.input_tokens',   500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3c', 'tool_name': ''}),
  ('2026-05-11 10:02:00', 'llm.output_tokens',  500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3d', 'tool_name': ''}),
  ('2026-05-11 10:02:00', 'llm.input_tokens',   500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3d', 'tool_name': ''}),
  ('2026-05-11 10:02:10', 'llm.output_tokens', 4000.0, 'claudecode', 'agent-sus', 'dep-test', 's4-sustained', {'run_id': 'r4c', 'tool_name': ''}),
  ('2026-05-11 10:02:10', 'llm.input_tokens',   500.0, 'claudecode', 'agent-sus', 'dep-test', 's4-sustained', {'run_id': 'r4c', 'tool_name': ''}),
  ('2026-05-11 10:02:30', 'llm.output_tokens',  500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3e', 'tool_name': ''}),
  ('2026-05-11 10:02:30', 'llm.input_tokens',   500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3e', 'tool_name': ''}),
  ('2026-05-11 10:03:00', 'llm.output_tokens', 5000.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3-spike',   'tool_name': ''}),
  ('2026-05-11 10:03:00', 'llm.input_tokens',   500.0, 'claudecode', 'agent-rel', 'dep-test', 's3-relative',  {'run_id': 'r3-spike',   'tool_name': ''}),
  ('2026-05-11 10:04:30', 'llm.output_tokens', 4000.0, 'claudecode', 'agent-bnd', 'dep-test', 's5-boundary',  {'run_id': 'r5a', 'tool_name': ''}),
  ('2026-05-11 10:04:30', 'llm.input_tokens',   500.0, 'claudecode', 'agent-bnd', 'dep-test', 's5-boundary',  {'run_id': 'r5a', 'tool_name': ''}),
  ('2026-05-11 10:05:30', 'llm.output_tokens', 4000.0, 'claudecode', 'agent-bnd', 'dep-test', 's5-boundary',  {'run_id': 'r5b', 'tool_name': ''}),
  ('2026-05-11 10:05:30', 'llm.input_tokens',   500.0, 'claudecode', 'agent-bnd', 'dep-test', 's5-boundary',  {'run_id': 'r5b', 'tool_name': ''}),
  ('2026-05-11 10:05:30', 'llm.output_tokens', 4000.0, 'claudecode', 'agent-bnd', 'dep-test', 's5-boundary',  {'run_id': 'r5c', 'tool_name': ''}),
  ('2026-05-11 10:05:30', 'llm.input_tokens',   500.0, 'claudecode', 'agent-bnd', 'dep-test', 's5-boundary',  {'run_id': 'r5c', 'tool_name': ''}),
  ('2026-05-11 11:00:00', 'llm.output_tokens', 5000.0, 'claudecode', 'agent-drip','dep-test', 's6-drip',      {'run_id': 'r6a', 'tool_name': ''}),
  ('2026-05-11 11:00:00', 'llm.input_tokens',   500.0, 'claudecode', 'agent-drip','dep-test', 's6-drip',      {'run_id': 'r6a', 'tool_name': ''}),
  ('2026-05-11 11:08:00', 'llm.output_tokens', 5000.0, 'claudecode', 'agent-drip','dep-test', 's6-drip',      {'run_id': 'r6b', 'tool_name': ''}),
  ('2026-05-11 11:08:00', 'llm.input_tokens',   500.0, 'claudecode', 'agent-drip','dep-test', 's6-drip',      {'run_id': 'r6b', 'tool_name': ''}),
  ('2026-05-11 11:16:00', 'llm.output_tokens', 5000.0, 'claudecode', 'agent-drip','dep-test', 's6-drip',      {'run_id': 'r6c', 'tool_name': ''}),
  ('2026-05-11 11:16:00', 'llm.input_tokens',   500.0, 'claudecode', 'agent-drip','dep-test', 's6-drip',      {'run_id': 'r6c', 'tool_name': ''}),
  ('2026-05-11 11:30:00', 'llm.output_tokens',    1.0, 'claudecode', 'agent-trailer', 'dep-test', 's-trailer', {'run_id': 'r-trailer', 'tool_name': ''}),
  ('2026-05-11 11:30:00', 'llm.input_tokens',     1.0, 'claudecode', 'agent-trailer', 'dep-test', 's-trailer', {'run_id': 'r-trailer', 'tool_name': ''});
