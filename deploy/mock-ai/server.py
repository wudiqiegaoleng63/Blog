#!/usr/bin/env python3
import hashlib
import json
import math
import os
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HOST = "0.0.0.0"
PORT = int(os.getenv("MOCK_AI_PORT", "8080"))
API_KEY = os.getenv("MOCK_AI_API_KEY", "mock-ai-test-only-key")
MAX_BODY_BYTES = 2 * 1024 * 1024
MAX_DIMENSIONS = 4096
MAX_INPUTS = 128
MAX_INPUT_CHARS = 100_000


def valid_text(value, max_chars=None):
    if not isinstance(value, str) or (max_chars is not None and len(value) > max_chars):
        return False
    try:
        value.encode("utf-8")
    except UnicodeEncodeError:
        return False
    return True


def tokens(value):
    normalized = " ".join(value.lower().split())
    words = re.findall(r"\w+", normalized, flags=re.UNICODE)
    characters = [char for char in normalized if char.isalnum()]
    bigrams = ["".join(characters[index:index + 2]) for index in range(len(characters) - 1)]
    return words + characters + bigrams or [normalized]


def embedding(value, dimensions):
    vector = [0.0] * dimensions
    for token in tokens(value):
        digest = hashlib.blake2b(token.encode("utf-8"), digest_size=16).digest()
        index = int.from_bytes(digest[:8], "big") % dimensions
        vector[index] += 1.0 if digest[8] & 1 else -1.0
    norm = math.sqrt(sum(component * component for component in vector))
    if norm == 0:
        vector[0] = 1.0
        return vector
    return [component / norm for component in vector]


def extract_sources(messages):
    user_message = next((item.get("content", "") for item in reversed(messages) if item.get("role") == "user"), "")
    context, _, question = user_message.rpartition("User question:\n")
    source_number = re.search(r"^\[SOURCE (\d+)]$", context, flags=re.MULTILINE)
    content_lines = [
        line for line in context.splitlines()
        if not re.fullmatch(r"\[/?SOURCE \d+]", line) and not line.startswith("Published article excerpts")
    ]
    content = "\n".join(content_lines).strip()
    sources = [(source_number.group(1), content)] if source_number and content else []
    return sources, question.strip()


def mock_answer(messages):
    sources, question = extract_sources(messages)
    if not sources:
        return "Mock AI could not find enough published article context to answer this question."
    number, content = sources[0]
    excerpt = " ".join(content.split())
    if len(excerpt) > 320:
        excerpt = excerpt[:320].rstrip() + "…"
    prefix = f"Mock AI answer for “{question}”: " if question else "Mock AI answer: "
    return f"{prefix}{excerpt} [{number}]"


class Handler(BaseHTTPRequestHandler):
    server_version = "BlogMockAI/1.0"

    def do_GET(self):
        if self.path == "/healthz":
            self.respond(200, {"status": "ok", "service": "blog-mock-ai"})
            return
        self.respond(404, {"error": {"message": "route not found"}})

    def do_POST(self):
        self.connection.settimeout(10)
        if self.headers.get("Authorization") != f"Bearer {API_KEY}":
            self.respond(401, {"error": {"message": "invalid mock API key"}})
            return
        payload = self.read_json()
        if payload is None:
            return
        if not isinstance(payload, dict):
            self.respond(400, {"error": {"message": "JSON body must be an object"}})
            return
        if self.path == "/v1/embeddings":
            self.handle_embeddings(payload)
            return
        if self.path == "/v1/chat/completions":
            self.handle_chat(payload)
            return
        self.respond(404, {"error": {"message": "route not found"}})

    def read_json(self):
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self.respond(400, {"error": {"message": "invalid content length"}})
            return None
        if length <= 0 or length > MAX_BODY_BYTES:
            self.respond(413, {"error": {"message": "request body size is invalid"}})
            return None
        try:
            return json.loads(self.rfile.read(length))
        except (UnicodeDecodeError, json.JSONDecodeError):
            self.respond(400, {"error": {"message": "invalid JSON body"}})
            return None

    def handle_embeddings(self, payload):
        dimensions = payload.get("dimensions")
        inputs = payload.get("input")
        model = payload.get("model")
        if not valid_text(model) or not model.strip():
            self.respond(400, {"error": {"message": "model is required"}})
            return
        if isinstance(dimensions, bool) or not isinstance(dimensions, int) or dimensions < 1 or dimensions > MAX_DIMENSIONS:
            self.respond(400, {"error": {"message": f"dimensions must be between 1 and {MAX_DIMENSIONS}"}})
            return
        if not isinstance(inputs, list) or not inputs or len(inputs) > MAX_INPUTS:
            self.respond(400, {"error": {"message": "input must be a non-empty bounded array"}})
            return
        if any(not valid_text(item, MAX_INPUT_CHARS) for item in inputs):
            self.respond(400, {"error": {"message": "embedding input is invalid"}})
            return
        data = [
            {"object": "embedding", "index": index, "embedding": embedding(item, dimensions)}
            for index, item in enumerate(inputs)
        ]
        self.respond(200, {"object": "list", "model": model, "data": data})

    def handle_chat(self, payload):
        model = payload.get("model")
        messages = payload.get("messages")
        max_tokens = payload.get("max_tokens")
        if not valid_text(model) or not model.strip() or not isinstance(messages, list) or not messages:
            self.respond(400, {"error": {"message": "model and messages are required"}})
            return
        if isinstance(max_tokens, bool) or not isinstance(max_tokens, int) or max_tokens < 1:
            self.respond(400, {"error": {"message": "max_tokens must be a positive integer"}})
            return
        if any(not isinstance(item, dict) or not valid_text(item.get("content")) for item in messages):
            self.respond(400, {"error": {"message": "messages are invalid"}})
            return
        answer = mock_answer(messages)
        max_chars = max_tokens * 4
        truncated = len(answer) > max_chars
        if truncated:
            answer = answer[:max_chars].rstrip()
        self.respond(200, {
            "id": "chatcmpl-blog-mock",
            "object": "chat.completion",
            "model": model,
            "choices": [{"index": 0, "message": {"role": "assistant", "content": answer}, "finish_reason": "length" if truncated else "stop"}],
        })

    def respond(self, status, payload):
        body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, pattern, *args):
        if self.path == "/healthz":
            return
        print(f"mock-ai {self.address_string()} {pattern % args}", flush=True)


if __name__ == "__main__":
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()
