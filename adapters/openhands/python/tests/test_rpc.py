import io
import json
import os
import threading
import urllib.request

from bc_openhands.llmproxy import start_proxy
from bc_openhands.main import summarize_event
from bc_openhands.rpc import Peer, RPCError


def pipe_pair():
    r1, w1 = os.pipe()
    r2, w2 = os.pipe()
    a = Peer(os.fdopen(r1, "r"), os.fdopen(w2, "w"))
    b = Peer(os.fdopen(r2, "r"), os.fdopen(w1, "w"))
    for p in (a, b):
        threading.Thread(target=p.serve, daemon=True).start()
    return a, b


def test_bidirectional_calls_and_errors():
    a, b = pipe_pair()
    b.register("add", lambda p: p["x"] + p["y"])
    a.register("echo", lambda p: p)

    def boom(_p):
        raise ValueError("nope")

    b.register("boom", boom)
    assert a.call("add", {"x": 2, "y": 3}, timeout=5) == 5
    assert b.call("echo", {"k": "v"}, timeout=5) == {"k": "v"}
    try:
        a.call("boom", {}, timeout=5)
        raise AssertionError("expected error")
    except RPCError as e:
        assert "nope" in e.message
    try:
        a.call("missing", {}, timeout=5)
        raise AssertionError("expected error")
    except RPCError as e:
        assert e.code == -32601


def test_notifications():
    a, b = pipe_pair()
    got = threading.Event()
    seen = {}

    def on_event(p):
        seen.update(p)
        got.set()

    b.register("event", on_event)
    a.notify("event", {"kind": "X"})
    assert got.wait(5) and seen == {"kind": "X"}


def test_proxy_tunnels_over_rpc():
    a, b = pipe_pair()
    calls = []

    def complete(p):
        calls.append(p)
        return {"status": 200, "body": {"choices": [{"message": {"role": "assistant", "content": "hi"}}]}}

    b.register("llm.complete", complete)
    server = start_proxy(a, "m", timeout=10)
    port = server.server_address[1]
    req = urllib.request.Request(
        f"http://127.0.0.1:{port}/v1/chat/completions",
        data=json.dumps({"model": "m", "messages": []}).encode(),
        headers={"Content-Type": "application/json"},
    )
    body = json.loads(urllib.request.urlopen(req, timeout=10).read())
    assert body["choices"][0]["message"]["content"] == "hi"
    assert calls[0]["path"] == "/v1/chat/completions"
    models = json.loads(urllib.request.urlopen(f"http://127.0.0.1:{port}/v1/models", timeout=10).read())
    assert models["data"][0]["id"] == "m"
    server.shutdown()


def test_summarize_unknown_event():
    class Weird:
        id = "1"
        source = "agent"

    assert summarize_event(Weird())["kind"] == "Weird"
