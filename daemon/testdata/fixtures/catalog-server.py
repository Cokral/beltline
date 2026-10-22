#!/usr/bin/env python3
"""Captured minimal catalog wire responses, never a work/session protocol."""
import json
import sys

framed = "--headless" in sys.argv
claude = "--input-format" in sys.argv
if claude:
    assert "--no-session-persistence" in sys.argv and "--bare" not in sys.argv
else:
    assert framed or "app-server" in sys.argv

def read():
    if framed:
        header = sys.stdin.buffer.readline().decode().strip()
        if not header:
            return None
        assert header.startswith("Content-Length:")
        size = int(header.split(":")[1])
        assert sys.stdin.buffer.readline() == b"\r\n"
        return json.loads(sys.stdin.buffer.read(size))
    line = sys.stdin.buffer.readline()
    return json.loads(line) if line else None

def send(value):
    data = json.dumps(value).encode()
    if framed:
        sys.stdout.buffer.write(f"Content-Length: {len(data)}\r\n\r\n".encode() + data)
    else:
        sys.stdout.buffer.write(data + b"\n")
    sys.stdout.buffer.flush()

while True:
    request = read()
    if request is None:
        break
    if claude:
        assert request["type"] == "control_request"
        assert request["request"] == {"subtype": "initialize"}
        send({"type": "control_response", "response": {"subtype": "success", "request_id": request["request_id"], "response": {"models": [{"value": "claude-exact", "displayName": "Claude fixture", "description": "Catalog only"}]}}})
        continue
    method = request["method"]
    assert method in {"initialize", "initialized", "model/list", "connect", "ping", "models.list"}
    if method == "initialized":
        continue
    if method == "model/list":
        cursor = request["params"].get("cursor")
        if cursor is None:
            result = {"data": [{"id": "internal-id", "model": "codex-exact", "displayName": "Codex fixture"}], "nextCursor": "page-two"}
        else:
            assert cursor == "page-two"
            result = {"data": [{"id": "second-id", "model": "codex-second", "displayName": "Second fixture"}], "nextCursor": None}
    elif method == "models.list":
        result = {"models": [{"id": "copilot-exact", "name": "Copilot fixture"}]}
    else:
        result = {"protocolVersion": 2}
    send({"jsonrpc": "2.0", "id": request["id"], "result": result})
