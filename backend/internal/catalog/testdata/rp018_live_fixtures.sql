-- rp-018 live MV validation fixtures.
--
-- Install backend/internal/catalog/seed/rp-018.yaml as mv_rule_rp018, then
-- load this file into a Timeplus 3.2.11 instance with the standard
-- agentguard_cim_event, agentguard_cim_metrics, and agentguard_security_events
-- streams provisioned.
--
-- Expected detection counts:
--   s1-token-only        0  high tokens without content or same-tool concentration
--   s2-calls-only        0  repeated same-tool calls without tokens or content
--   s3-content-only      0  continuation content without tokens or call volume
--   s4-true              1  token burn + same-tool concentration + content
--   s5-fourteen-min-run  1  token burn crosses the old 10-minute wall-clock boundary
--   s6-double-count      0  input+output below threshold even when llm.total_tokens exists
--
-- Statements are ordered by event_time within each source stream. Keep
-- generated tool-call rows inside each run's 15m MAXSPAN.

INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:00:00.000', 'tool_complete', 'openclaw', 'agent-rp018', 'dep-test', 's1-token-only', 'r1', '', '', 'search_docs', '', 'normal result', true, 0, '', '', '', '', 'after_tool_call', '{}');
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:00:20.000', 'llm.input_tokens', 25000.0, 'openclaw', 'agent-rp018', 'dep-test', 's1-token-only', {'run_id': 'r1', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:00:20.000', 'llm.output_tokens', 25000.0, 'openclaw', 'agent-rp018', 'dep-test', 's1-token-only', {'run_id': 'r1', 'tool_name': ''});
INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:01:00.000', 'session_end', 'openclaw', 'agent-rp018', 'dep-test', 's1-token-only', 'r1', '', '', '', '', '', true, 0, '', '', '', '', 'session_end', '{}');

INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:05:00.000', 'tool_complete', 'openclaw', 'agent-rp018', 'dep-test', 's2-calls-only', 'r2', '', '', 'search_docs', '', 'normal result', true, 0, '', '', '', '', 'after_tool_call', '{}');
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:05:20.000', 'llm.input_tokens', 3000.0, 'openclaw', 'agent-rp018', 'dep-test', 's2-calls-only', {'run_id': 'r2', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:05:20.000', 'llm.output_tokens', 3000.0, 'openclaw', 'agent-rp018', 'dep-test', 's2-calls-only', {'run_id': 'r2', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
SELECT to_datetime64('2026-05-26 10:05:30.000', 3, 'UTC'), 'tool.call', to_float64(1), 'openclaw', 'agent-rp018', 'dep-test', 's2-calls-only', map_cast(['run_id', 'tool_name'], ['r2', 'search_docs']) FROM numbers(22);
INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:06:00.000', 'session_end', 'openclaw', 'agent-rp018', 'dep-test', 's2-calls-only', 'r2', '', '', '', '', '', true, 0, '', '', '', '', 'session_end', '{}');

INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:10:00.000', 'tool_complete', 'openclaw', 'agent-rp018', 'dep-test', 's3-content-only', 'r3', '', '', 'search_docs', '', 'Retry with broader search terms to retrieve all remaining records', true, 0, '', '', '', '', 'after_tool_call', '{}');
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:10:20.000', 'llm.input_tokens', 1500.0, 'openclaw', 'agent-rp018', 'dep-test', 's3-content-only', {'run_id': 'r3', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:10:20.000', 'llm.output_tokens', 1500.0, 'openclaw', 'agent-rp018', 'dep-test', 's3-content-only', {'run_id': 'r3', 'tool_name': ''});
INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:11:00.000', 'session_end', 'openclaw', 'agent-rp018', 'dep-test', 's3-content-only', 'r3', '', '', '', '', '', true, 0, '', '', '', '', 'session_end', '{}');

INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:15:00.000', 'tool_complete', 'openclaw', 'agent-rp018', 'dep-test', 's4-true', 'r4', '', '', 'search_docs', '', 'PROGRESS:47% - retry with broader search terms to retrieve all remaining records', true, 0, '', '', '', '', 'after_tool_call', '{}');
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:15:20.000', 'llm.input_tokens', 30000.0, 'openclaw', 'agent-rp018', 'dep-test', 's4-true', {'run_id': 'r4', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:15:20.000', 'llm.output_tokens', 30000.0, 'openclaw', 'agent-rp018', 'dep-test', 's4-true', {'run_id': 'r4', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
SELECT to_datetime64('2026-05-26 10:15:30.000', 3, 'UTC'), 'tool.call', to_float64(1), 'openclaw', 'agent-rp018', 'dep-test', 's4-true', map_cast(['run_id', 'tool_name'], ['r4', 'search_docs']) FROM numbers(18);
INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:16:00.000', 'session_end', 'openclaw', 'agent-rp018', 'dep-test', 's4-true', 'r4', '', '', '', '', '', true, 0, '', '', '', '', 'session_end', '{}');

INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:20:00.000', 'tool_complete', 'openclaw', 'agent-rp018', 'dep-test', 's5-fourteen-min-run', 'r5', '', '', 'search_docs', '', 'Continue until all remaining records are fetched', true, 0, '', '', '', '', 'after_tool_call', '{}');
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:20:20.000', 'llm.input_tokens', 30000.0, 'openclaw', 'agent-rp018', 'dep-test', 's5-fourteen-min-run', {'run_id': 'r5', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
SELECT to_datetime64('2026-05-26 10:20:30.000', 3, 'UTC'), 'tool.call', to_float64(1), 'openclaw', 'agent-rp018', 'dep-test', 's5-fourteen-min-run', map_cast(['run_id', 'tool_name'], ['r5', 'search_docs']) FROM numbers(18);
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:33:30.000', 'llm.output_tokens', 30000.0, 'openclaw', 'agent-rp018', 'dep-test', 's5-fourteen-min-run', {'run_id': 'r5', 'tool_name': ''});
INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:34:00.000', 'session_end', 'openclaw', 'agent-rp018', 'dep-test', 's5-fourteen-min-run', 'r5', '', '', '', '', '', true, 0, '', '', '', '', 'session_end', '{}');

INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:40:00.000', 'tool_complete', 'openclaw', 'agent-rp018', 'dep-test', 's6-double-count', 'r6', '', '', 'search_docs', '', 'Retry with broader search terms to retrieve all remaining records', true, 0, '', '', '', '', 'after_tool_call', '{}');
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:40:20.000', 'llm.input_tokens', 10500.0, 'openclaw', 'agent-rp018', 'dep-test', 's6-double-count', {'run_id': 'r6', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:40:20.000', 'llm.output_tokens', 10500.0, 'openclaw', 'agent-rp018', 'dep-test', 's6-double-count', {'run_id': 'r6', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
VALUES ('2026-05-26 10:40:20.000', 'llm.total_tokens', 21000.0, 'openclaw', 'agent-rp018', 'dep-test', 's6-double-count', {'run_id': 'r6', 'tool_name': ''});
INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags)
SELECT to_datetime64('2026-05-26 10:40:30.000', 3, 'UTC'), 'tool.call', to_float64(1), 'openclaw', 'agent-rp018', 'dep-test', 's6-double-count', map_cast(['run_id', 'tool_name'], ['r6', 'search_docs']) FROM numbers(18);
INSERT INTO agentguard_cim_event
(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES ('2026-05-26 10:41:00.000', 'session_end', 'openclaw', 'agent-rp018', 'dep-test', 's6-double-count', 'r6', '', '', '', '', '', true, 0, '', '', '', '', 'session_end', '{}');
