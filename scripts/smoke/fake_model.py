#!/usr/bin/env python3
"""A scripted OpenAI-compatible chat-completions server for smoke tests.

It lets the first-task smoke test exercise the real CLI, gateway, OpenHands
adapter, sandbox and verification without downloading a model or using a
cloud account. It is not a model: replies are fixed.

* Requests without tools come from the control plane (the task contract):
  the reply is the JSON contract in CONTRACT.
* Agent requests (with tools): when the last message is an instruction
  (the task, or BoundedCode's retry message), the reply calls the terminal
  tool with the next command from the file named by --command-file;
  after the command's result it calls the finish tool. The file holds one
  command, or several separated by lines containing only "---": the first
  for the task, the next for the first retry, and so on (the last one
  repeats). Tool names and required arguments are read from the request's
  tool schemas.

Every request is appended, summarised, to --log. The server listens on
127.0.0.1 only and prints the port it bound on stdout.

usage: fake_model.py --command-file FILE --log FILE [--port N]
"""
import argparse
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

CONTRACT = {
    "required": ["Total applies the 10% bulk discount to orders of exactly 10 items"],
    "acceptable_alternatives": [],
    "constraints": [],
    "explicitly_not_required": [],
    "unknown_or_ambiguous": [],
    "acceptance_evidence": ["a test that Total of 10 items at 100 cents is 900"],
}


def fill_required(schema, given):
    """Completes the arguments a tool schema requires with neutral values."""
    args = dict(given)
    props = (schema or {}).get("properties", {})
    for name in (schema or {}).get("required", []):
        if name in args:
            continue
        p = props.get(name, {})
        if "enum" in p:
            args[name] = "LOW" if "LOW" in p["enum"] else p["enum"][0]
        elif p.get("type") == "boolean":
            args[name] = False
        elif p.get("type") in ("integer", "number"):
            args[name] = 0
        elif p.get("type") == "array":
            args[name] = []
        else:
            args[name] = ""
    return args


def pick(tools, *needles):
    for t in tools:
        fn = t.get("function", {})
        if any(n in fn.get("name", "") for n in needles):
            return fn
    return None


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):  # quiet
        pass

    def do_GET(self):
        if self.path.rstrip("/").endswith("/models"):
            return self.reply({"object": "list", "data": [{"id": "smoke", "object": "model"}]})
        self.send_error(404)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        tools = body.get("tools") or []
        msgs = body.get("messages") or []
        last = msgs[-1].get("role") if msgs else ""
        turn = sum(1 for m in msgs if m.get("role") == "user") - 1
        if not tools:
            kind, message = "control", {"role": "assistant", "content": json.dumps(CONTRACT)}
            finish = "stop"
        else:
            term, fin = pick(tools, "terminal", "bash", "execute"), pick(tools, "finish")
            if last != "tool" and term:
                cmds = [c.strip() for c in open(self.server.command_file).read().split("\n---\n")]
                cmd = cmds[min(max(turn, 0), len(cmds) - 1)]
                call = (term["name"], fill_required(term.get("parameters"), {"command": cmd}))
                kind = "agent:command %d" % turn
            else:
                call = (fin["name"], fill_required(fin.get("parameters"), {"message": "Done."}))
                kind = "agent:finish"
            message = {"role": "assistant", "content": None, "tool_calls": [{
                "id": "call_%d" % int(time.time() * 1000), "type": "function",
                "function": {"name": call[0], "arguments": json.dumps(call[1])}}]}
            finish = "tool_calls"
        with open(self.server.log, "a") as f:
            f.write(json.dumps({"kind": kind, "tools": [t.get("function", {}).get("name") for t in tools],
                                "messages": len(msgs)}) + "\n")
        self.reply({"id": "smoke-%d" % time.time_ns(), "object": "chat.completion", "created": int(time.time()),
                    "model": body.get("model", "smoke"),
                    "choices": [{"index": 0, "message": message, "finish_reason": finish}],
                    "usage": {"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}})

    def reply(self, obj):
        b = json.dumps(obj).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--command-file", required=True)
    ap.add_argument("--log", required=True)
    ap.add_argument("--port", type=int, default=0)
    a = ap.parse_args()
    srv = ThreadingHTTPServer(("127.0.0.1", a.port), Handler)
    srv.command_file, srv.log = a.command_file, a.log
    print(srv.server_address[1], flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    sys.exit(main())
