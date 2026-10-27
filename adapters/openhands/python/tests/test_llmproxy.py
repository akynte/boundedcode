"""The proxy tells the condenser's requests from the agent's."""

from __future__ import annotations

import json
import threading
import urllib.request
from typing import Any

from bc_openhands.llmproxy import CONDENSER_PREFIX, split_purpose, start_proxy


def test_split_purpose() -> None:
    assert split_purpose("/v1/chat/completions") == ("", "/v1/chat/completions")
    assert split_purpose(CONDENSER_PREFIX + "/v1/chat/completions") == ("condenser", "/v1/chat/completions")
    assert split_purpose("/condensers/v1") == ("", "/condensers/v1")


class FakePeer:
    def __init__(self) -> None:
        self.calls: list[dict[str, Any]] = []
        self.lock = threading.Lock()

    def call(self, method: str, params: dict[str, Any], timeout: float) -> dict[str, Any]:
        with self.lock:
            self.calls.append({"method": method, **params})
        return {"status": 200, "body": {"ok": True}}


def post(port: int, path: str) -> None:
    req = urllib.request.Request(
        f"http://127.0.0.1:{port}{path}",
        data=json.dumps({"messages": []}).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=5) as r:
        assert r.status == 200


def test_proxy_forwards_purpose() -> None:
    peer = FakePeer()
    server = start_proxy(peer, "m", timeout=5)  # type: ignore[arg-type]
    try:
        port = server.server_address[1]
        post(port, "/v1/chat/completions")
        post(port, CONDENSER_PREFIX + "/v1/chat/completions")
    finally:
        server.shutdown()
    agent, condenser = peer.calls
    assert agent["path"] == "/v1/chat/completions" and "purpose" not in agent
    assert condenser["path"] == "/v1/chat/completions" and condenser["purpose"] == "condenser"
