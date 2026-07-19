#!/usr/bin/env python3
import json
import os
import sys
import time
import urllib.error
import urllib.request

API_URL = os.getenv("BLOG_API_URL", "http://api:8080")
MOCK_AI_URL = os.getenv("MOCK_AI_URL", "http://mock-ai:8080")
MOCK_AI_API_KEY = os.getenv("MOCK_AI_API_KEY", "mock-ai-test-only-key")
TIMEOUT_SECONDS = int(os.getenv("MOCK_AI_SMOKE_TIMEOUT_SECONDS", "120"))


def post_json(url, payload, headers=None):
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json", **(headers or {})},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        return response.status, json.load(response)


def check_chat():
    status, payload = post_json(
        f"{MOCK_AI_URL}/v1/chat/completions",
        {
            "model": "blog-mock-chat",
            "messages": [
                {"role": "system", "content": "Mock startup check"},
                {"role": "user", "content": "Published article excerpts:\n\n[SOURCE 1]\nMock startup context\n[/SOURCE 1]\n\nUser question:\nWhat is this?"},
            ],
            "max_tokens": 100,
            "stream": False,
        },
        {"Authorization": f"Bearer {MOCK_AI_API_KEY}"},
    )
    content = payload.get("choices", [{}])[0].get("message", {}).get("content", "")
    if status != 200 or "Mock startup context" not in content or "[1]" not in content:
        raise RuntimeError(f"unexpected Mock Chat response: {payload!r}")


def check_ask():
    status, payload = post_json(f"{API_URL}/api/v1/ai/ask", {"question": "mock AI startup check"})
    data = payload.get("data", {})
    if status != 200 or not isinstance(data.get("answer"), str) or not isinstance(data.get("sources"), list):
        raise RuntimeError(f"unexpected Ask response: {payload!r}")


def main():
    deadline = time.monotonic() + TIMEOUT_SECONDS
    last_error = None
    while time.monotonic() < deadline:
        try:
            check_chat()
            check_ask()
            print("Mock AI Chat and Ask smoke passed.")
            return 0
        except (OSError, ValueError, RuntimeError, urllib.error.HTTPError) as error:
            last_error = error
            time.sleep(2)
    print(f"Mock AI smoke failed: {last_error}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
