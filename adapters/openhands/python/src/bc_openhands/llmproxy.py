"""Loopback HTTP proxy that tunnels OpenAI-compatible requests over RPC.

LiteLLM inside the OpenHands SDK talks HTTP to 127.0.0.1:<port>; each request
is forwarded to the Go control plane as an `llm.complete` call, so the
sandbox needs no network access and the control plane meters every call.
"""

from __future__ import annotations

import json
import logging
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from .rpc import Peer, RPCError

log = logging.getLogger("bc_openhands.llmproxy")


def start_proxy(peer: Peer, model_alias: str, timeout: float) -> ThreadingHTTPServer:
    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt: str, *args: Any) -> None:  # silence default stderr spam
            log.debug(fmt, *args)

        def _reply(self, status: int, body: Any) -> None:
            data = body if isinstance(body, bytes) else json.dumps(body).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self) -> None:  # noqa: N802
            if self.path.rstrip("/").endswith("/models"):
                self._reply(200, {"object": "list", "data": [{"id": model_alias, "object": "model"}]})
            else:
                self._reply(404, {"error": {"message": "not found"}})

        def do_POST(self) -> None:  # noqa: N802
            length = int(self.headers.get("Content-Length") or 0)
            raw = self.rfile.read(length)
            try:
                body = json.loads(raw or b"{}")
            except json.JSONDecodeError:
                self._reply(400, {"error": {"message": "invalid JSON"}})
                return
            if body.get("stream"):
                # Streaming is not tunnelled (ADR-0004); the adapter configures
                # the SDK with stream=False, so this indicates a misconfiguration.
                self._reply(400, {"error": {"message": "streaming not supported by boundedcode tunnel"}})
                return
            try:
                result = peer.call("llm.complete", {"path": self.path, "body": body}, timeout=timeout)
            except RPCError as e:
                self._reply(502, {"error": {"message": f"control plane: {e.message}", "type": "bc_tunnel"}})
                return
            except TimeoutError:
                self._reply(504, {"error": {"message": "control plane timeout"}})
                return
            self._reply(int(result.get("status", 200)), result.get("body", {}))

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    threading.Thread(target=server.serve_forever, name="llm-proxy", daemon=True).start()
    return server
