#!/usr/bin/env python3
"""Mock OpenAI-compatible endpoint for Semantic DLP verification.

Serves deterministic responses with no dependencies:
  POST /v1/embeddings        -> DIM-dim vectors derived from input hash
  POST /v1/chat/completions  -> fixed risk JSON / canned completion
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
                return self._stream_chat(msgs)
            prompt = msgs[-1].get("content", "") if msgs else ""
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
        else:
            self.send_response(404)
            self.end_headers()
            return
        self._send_json(resp)

    def _sse(self, obj):
        self.wfile.write(b"data: " + json.dumps(obj).encode() + b"\n\n")

    def _stream_chat(self, msgs):
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
