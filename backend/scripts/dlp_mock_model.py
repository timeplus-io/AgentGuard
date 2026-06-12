#!/usr/bin/env python3
"""Mock OpenAI-compatible endpoint for Semantic DLP verification.

Serves deterministic responses with no dependencies:
  POST /v1/embeddings        -> DIM-dim vectors derived from input hash
  POST /v1/chat/completions  -> fixed risk JSON / canned completion

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


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path.endswith("/embeddings"):
            inputs = body["input"]
            if isinstance(inputs, str):
                inputs = [inputs]
            resp = {"data": [{"index": i, "embedding": embed(t)} for i, t in enumerate(inputs)]}
        elif self.path.endswith("/chat/completions"):
            msgs = body.get("messages", [])
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
        out = json.dumps(resp).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(out)))
        self.end_headers()
        self.wfile.write(out)

    def log_message(self, fmt, *args):
        sys.stderr.write("[mock-model] " + fmt % args + "\n")


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 4000
    if len(sys.argv) > 2:
        DIM = int(sys.argv[2])
    print(f"mock model endpoint on :{port} (dim={DIM})")
    HTTPServer(("0.0.0.0", port), Handler).serve_forever()
