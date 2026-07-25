#!/usr/bin/env python3
"""Mock OpenAI-compatible endpoint for Semantic DLP verification.

Serves deterministic responses with no dependencies:
  POST /v1/embeddings        -> DIM-dim vectors derived from input hash
  POST /v1/chat/completions  -> fixed risk JSON / canned completion
  POST /v1/messages          -> Anthropic Messages API SSE stream (Sentry's
                                 anthropic-sdk-go provider; tool_use block
                                 when body.tools is non-empty, else text)
  GET  /v1/models, /models   -> model list (used by POST /api/config/test-dlp)
  GET  /, /health            -> liveness probe ("ok") for the integration scripts

Usage: python3 backend/scripts/dlp_mock_model.py [port] [dim]   (default 4000, 8)
"""
import hashlib
import json
import re
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

# DIM defaults to 8 for fast verification; set dlp.embedding_dim to match in
# config.yaml when using this mock.
DIM = 8


def embed(text):
    h = hashlib.sha256(text.encode()).digest()
    return [round(b / 255.0 - 0.5, 4) for b in h[:DIM]]


MODEL_IDS = ("mock-embed", "mock-risk", "mock-summary")


class Handler(BaseHTTPRequestHandler):
    def _send_json(self, resp, code=200):
        out = json.dumps(resp).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(out)))
        self.end_headers()
        self.wfile.write(out)

    def do_GET(self):
        # /models and /v1/models back the POST /api/config/test-dlp reachability
        # probe; / and /health are liveness checks for the integration scripts.
        if self.path.rstrip("/").endswith("/models"):
            self._send_json({"object": "list",
                             "data": [{"id": m, "object": "model"} for m in MODEL_IDS]})
        elif self.path in ("/", "/health", "/healthz"):
            self._send_json({"status": "ok"})
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path.endswith("/embeddings"):
            inputs = body["input"]
            if isinstance(inputs, str):
                inputs = [inputs]
            resp = {"data": [{"index": i, "embedding": embed(t)} for i, t in enumerate(inputs)]}
        elif self.path.endswith("/chat/completions"):
            msgs = body.get("messages", [])
            if body.get("stream"):
                return self._stream_chat(msgs, body.get("tools") or [])
            prompt = msgs[-1].get("content", "") if msgs else ""
            tools = body.get("tools") or []
            if tools and body.get("tool_choice") in ("required", "auto") or (
                    tools and isinstance(body.get("tool_choice"), dict)):
                # Non-stream tool-calling probe (POST /api/sentry/config/test):
                # deterministically call the first offered tool with empty args.
                fn = tools[0].get("function", {})
                resp = {"choices": [{"message": {"content": None, "tool_calls": [
                    {"id": "call_1", "type": "function",
                     "function": {"name": fn.get("name", ""), "arguments": "{}"}}]}}]}
                self._send_json(resp)
                return
            if "TASK: QUERY_UNDERSTANDING" in prompt:
                content = json.dumps({"keywords": ["customer", "phone", "export"],
                                      "entity_types": ["phone"],
                                      "risk_tags": ["PII"],
                                      "time_range_days": 30})
            elif "TASK: RERANK" in prompt:
                m = re.search(r"ITEMS: (\d+)", prompt)
                n = int(m.group(1)) if m else 1
                content = json.dumps(
                    {"scores": [round(max(0.05, 0.95 - 0.05 * i), 2) for i in range(n)]})
            elif "TASK: AUDIT_SUMMARY" in prompt:
                content = ("Suspected leak: an agent exported customer phone numbers. "
                           "Evidence in the cited messages.")
            else:
                content = '{"score": 0.9, "tags": ["PII", "CUSTOMER_DATA"]}'
            resp = {"choices": [{"message": {"content": content}}]}
        elif self.path.endswith("/messages"):
            return self._stream_messages(body)
        else:
            self.send_response(404)
            self.end_headers()
            return
        self._send_json(resp)

    def _sse(self, obj):
        self.wfile.write(b"data: " + json.dumps(obj).encode() + b"\n\n")

    def _sse_anthropic(self, event_type, obj):
        # Anthropic Messages streaming frames are TWO lines: "event: <type>"
        # then "data: <json>", blank-line terminated. The plain "_sse" helper
        # above only emits "data:" (OpenAI chat/completions.chunk shape) —
        # do not reuse it here.
        self.wfile.write(b"event: " + event_type.encode() + b"\n")
        self.wfile.write(b"data: " + json.dumps(obj, separators=(",", ":")).encode() + b"\n\n")

    def _stream_messages(self, body):
        # Anthropic Messages API (POST /v1/messages), streamed SSE — backs
        # Sentry's anthropic-sdk-go provider (backend/internal/sentry/anthropic.go),
        # which always calls Messages.NewStreaming and drives msg.Accumulate
        # over the event sequence below. Anthropic tool schema is flat
        # (body.tools[i].name), NOT nested under "function" like OpenAI.
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        tools = body.get("tools") or []
        self._sse_anthropic("message_start", {
            "type": "message_start", "message": {
                "id": "msg_mock", "type": "message", "role": "assistant", "model": "mock-claude",
                "content": [], "stop_reason": None, "stop_sequence": None,
                "usage": {"input_tokens": 10, "output_tokens": 0}}})
        if tools:
            name = tools[0].get("name", "")
            self._sse_anthropic("content_block_start", {
                "type": "content_block_start", "index": 0,
                "content_block": {"type": "tool_use", "id": "toolu_mock1", "name": name, "input": {}}})
            self._sse_anthropic("content_block_delta", {
                "type": "content_block_delta", "index": 0,
                "delta": {"type": "input_json_delta", "partial_json": "{}"}})
            self._sse_anthropic("content_block_stop", {"type": "content_block_stop", "index": 0})
            self._sse_anthropic("message_delta", {
                "type": "message_delta",
                "delta": {"stop_reason": "tool_use", "stop_sequence": None},
                "usage": {"output_tokens": 5}})
        else:
            self._sse_anthropic("content_block_start", {
                "type": "content_block_start", "index": 0,
                "content_block": {"type": "text", "text": ""}})
            self._sse_anthropic("content_block_delta", {
                "type": "content_block_delta", "index": 0,
                "delta": {"type": "text_delta", "text": "MOCK_ANSWER"}})
            self._sse_anthropic("content_block_stop", {"type": "content_block_stop", "index": 0})
            self._sse_anthropic("message_delta", {
                "type": "message_delta",
                "delta": {"stop_reason": "end_turn", "stop_sequence": None},
                "usage": {"output_tokens": 5}})
        self._sse_anthropic("message_stop", {"type": "message_stop"})

    def _stream_chat(self, msgs, tools):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        base = {"id": "mock", "object": "chat.completion.chunk", "model": "mock-chat"}
        last_user = next((m["content"] for m in reversed(msgs)
                          if m.get("role") == "user" and isinstance(m.get("content"), str)), "")
        tool_results = [m for m in msgs if m.get("role") == "tool"]
        m = re.search(r"MOCK_TOOL_CALL (\{.*\})", last_user, re.S)
        if m and not tool_results:
            call = json.loads(m.group(1))
            self._sse({**base, "choices": [{"index": 0, "delta": {"role": "assistant", "tool_calls": [
                {"index": 0, "id": "call_1", "type": "function",
                 "function": {"name": call["name"], "arguments": json.dumps(call.get("arguments", {}))}}]},
                "finish_reason": None}]})
            self._sse({**base, "choices": [{"index": 0, "delta": {}, "finish_reason": "tool_calls"}],
                       "usage": {"prompt_tokens": 10, "completion_tokens": 5}})
        elif tools and not tool_results:
            # Streaming tool-forcing probe (POST /api/sentry/config/test via the
            # unified sentryProbeChat -> openaiLLM.ChatStream, which streams): no
            # MOCK_TOOL_CALL sentinel, just a natural-language "you must call X"
            # prompt. Emit a deterministic call to the first offered tool,
            # mirroring the non-stream tool branch and the /v1/messages handler.
            fn = tools[0].get("function", {})
            self._sse({**base, "choices": [{"index": 0, "delta": {"role": "assistant", "tool_calls": [
                {"index": 0, "id": "call_1", "type": "function",
                 "function": {"name": fn.get("name", ""), "arguments": "{}"}}]},
                "finish_reason": None}]})
            self._sse({**base, "choices": [{"index": 0, "delta": {}, "finish_reason": "tool_calls"}],
                       "usage": {"prompt_tokens": 10, "completion_tokens": 5}})
        else:
            content = "MOCK_ANSWER"
            if tool_results:
                content += " tool said: " + str(tool_results[-1].get("content", ""))[:80]
            self._sse({**base, "choices": [{"index": 0, "delta": {"role": "assistant", "content": content},
                                            "finish_reason": None}]})
            self._sse({**base, "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
                       "usage": {"prompt_tokens": 10, "completion_tokens": 5}})
        self.wfile.write(b"data: [DONE]\n\n")

    def log_message(self, fmt, *args):
        sys.stderr.write("[mock-model] " + fmt % args + "\n")


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 4000
    if len(sys.argv) > 2:
        DIM = int(sys.argv[2])
    print(f"mock model endpoint on :{port} (dim={DIM})")
    HTTPServer(("0.0.0.0", port), Handler).serve_forever()
