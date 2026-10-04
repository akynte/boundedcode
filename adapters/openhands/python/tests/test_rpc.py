import io
import json
import os
import threading
import time
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


def feed_peer(lines):
    """A peer reading the given raw lines; returns (peer, output buffer)."""
    out = io.StringIO()
    peer = Peer(io.StringIO("".join(line + "\n" for line in lines)), out)
    return peer, out


def test_malformed_lines_do_not_stop_the_stream():
    seen = threading.Event()
    peer, _out = feed_peer(
        [
            "not json",
            "[1, 2]",
            "5",
            '{"jsonrpc":"2.0","id":[1],"result":1}',
            '{"jsonrpc":"2.0","method":7,"params":{}}',
            '{"jsonrpc":"2.0","id":424242,"result":{}}',
            '{"jsonrpc":"2.0","method":"event","params":{"k":1}}',
        ]
    )
    peer.register("event", lambda p: seen.set())
    peer.serve()  # returns at EOF without raising
    assert seen.wait(5)
    assert peer.closed.is_set()


def test_unknown_method_and_invalid_method_get_errors():
    peer, out = feed_peer(
        [
            '{"jsonrpc":"2.0","id":1,"method":"nope","params":{}}',
            '{"jsonrpc":"2.0","id":2,"method":["x"]}',
            '{"jsonrpc":"2.0","method":"unknown.notification","params":{}}',
        ]
    )
    peer.serve()
    replies = {}
    for _ in range(50):  # request handlers run on worker threads
        replies = {m["id"]: m for m in map(json.loads, out.getvalue().splitlines())}
        if len(replies) == 2:
            break
        time.sleep(0.1)
    assert replies[1]["error"]["code"] == -32601
    assert replies[2]["error"]["code"] == -32600


def test_call_after_close_fails_fast():
    peer, _ = feed_peer([])
    peer.serve()
    try:
        peer.call("x", {}, timeout=1)
        raise AssertionError("expected error")
    except RPCError as e:
        assert "closed" in e.message


def test_ready_reports_protocol_version():
    from bc_openhands.main import PROTOCOL_VERSION, ready_params

    params = ready_params()
    assert params["protocol_version"] == PROTOCOL_VERSION == 1
    assert params["adapter"] == "bc-openhands"
