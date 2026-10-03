"""Newline-delimited JSON-RPC 2.0 peer over a pair of file objects.

Both sides may send requests. Incoming requests are dispatched on worker
threads (agent runs block for minutes); outgoing requests block the calling
thread until the matching response arrives. See ADR-0004.
"""

from __future__ import annotations

import itertools
import json
import logging
import threading
import traceback
from concurrent.futures import Future
from typing import Any, Callable, TextIO

log = logging.getLogger("bc_openhands.rpc")

Handler = Callable[[dict[str, Any]], Any]


class RPCError(Exception):
    def __init__(self, code: int, message: str, data: Any = None):
        super().__init__(message)
        self.code = code
        self.message = message
        self.data = data


METHOD_NOT_FOUND = -32601
INVALID_PARAMS = -32602
INTERNAL_ERROR = -32603
APP_ERROR = -32000


class Peer:
    def __init__(self, reader: TextIO, writer: TextIO):
        self._reader = reader
        self._writer = writer
        self._write_lock = threading.Lock()
        self._ids = itertools.count(1)
        self._pending: dict[int, Future] = {}
        self._pending_lock = threading.Lock()
        self._handlers: dict[str, Handler] = {}
        self.closed = threading.Event()

    def register(self, method: str, handler: Handler) -> None:
        self._handlers[method] = handler

    # ---- outgoing ----
    def _send(self, msg: dict[str, Any]) -> None:
        line = json.dumps(msg, separators=(",", ":"), ensure_ascii=False)
        with self._write_lock:
            self._writer.write(line + "\n")
            self._writer.flush()

    def notify(self, method: str, params: Any) -> None:
        if self.closed.is_set():
            return
        try:
            self._send({"jsonrpc": "2.0", "method": method, "params": params})
        except (BrokenPipeError, ValueError, OSError):
            self.closed.set()

    def call(self, method: str, params: Any, timeout: float | None = None) -> Any:
        if self.closed.is_set():
            raise RPCError(INTERNAL_ERROR, "peer closed")
        msg_id = next(self._ids)
        fut: Future = Future()
        with self._pending_lock:
            self._pending[msg_id] = fut
        try:
            self._send({"jsonrpc": "2.0", "id": msg_id, "method": method, "params": params})
            return fut.result(timeout=timeout)
        finally:
            with self._pending_lock:
                self._pending.pop(msg_id, None)

    # ---- incoming ----
    def serve(self) -> None:
        """Read messages until EOF. Blocks."""
        try:
            for line in self._reader:
                line = line.strip()
                if not line:
                    continue
                try:
                    msg = json.loads(line)
                except json.JSONDecodeError:
                    log.error("invalid JSON from peer: %.200s", line)
                    continue
                self._dispatch(msg)
        finally:
            self.closed.set()
            with self._pending_lock:
                for fut in self._pending.values():
                    if not fut.done():
                        fut.set_exception(RPCError(INTERNAL_ERROR, "peer closed"))

    def _dispatch(self, msg: dict[str, Any]) -> None:
        if "method" in msg:
            if "id" in msg:
                threading.Thread(target=self._handle_request, args=(msg,), daemon=True).start()
            else:
                handler = self._handlers.get(msg["method"])
                if handler:
                    threading.Thread(target=handler, args=(msg.get("params") or {},), daemon=True).start()
            return
        msg_id = msg.get("id")
        with self._pending_lock:
            fut = self._pending.get(msg_id)
        if fut is None:
            log.warning("response for unknown id %r", msg_id)
            return
        if "error" in msg and msg["error"] is not None:
            err = msg["error"]
            fut.set_exception(RPCError(err.get("code", APP_ERROR), err.get("message", ""), err.get("data")))
        else:
            fut.set_result(msg.get("result"))

    def _handle_request(self, msg: dict[str, Any]) -> None:
        msg_id = msg["id"]
        handler = self._handlers.get(msg["method"])
        if handler is None:
            self._reply_error(msg_id, METHOD_NOT_FOUND, f"unknown method {msg['method']}")
            return
        try:
            result = handler(msg.get("params") or {})
        except RPCError as e:
            self._reply_error(msg_id, e.code, e.message, e.data)
            return
        except Exception as e:  # noqa: BLE001 - errors must cross the boundary
            log.error("handler %s failed: %s", msg["method"], traceback.format_exc())
            self._reply_error(msg_id, APP_ERROR, f"{type(e).__name__}: {e}")
            return
        try:
            self._send({"jsonrpc": "2.0", "id": msg_id, "result": result})
        except (BrokenPipeError, ValueError, OSError):
            self.closed.set()

    def _reply_error(self, msg_id: Any, code: int, message: str, data: Any = None) -> None:
        err: dict[str, Any] = {"code": code, "message": message}
        if data is not None:
            err["data"] = data
        try:
            self._send({"jsonrpc": "2.0", "id": msg_id, "error": err})
        except (BrokenPipeError, ValueError, OSError):
            self.closed.set()
