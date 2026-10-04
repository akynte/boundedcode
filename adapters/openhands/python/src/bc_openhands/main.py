"""boundedcode <-> OpenHands SDK adapter.

Deliberately thin: it maps JSON-RPC methods onto one OpenHands Conversation
and reports events. Task lifecycle, retries, verification, routing and
escalation live in the Go control plane.

Protocol (ADR-0004), newline-delimited JSON-RPC 2.0 on stdin/stdout:
  Go -> adapter  session.open, session.send, session.interrupt, session.state,
                 session.condense, shutdown
  adapter -> Go  llm.complete (request), event (notification), ready (notification)

`ready` carries PROTOCOL_VERSION; the control plane refuses a mismatch.
"""

from __future__ import annotations

import os
import sys


def _claim_stdout():
    """Reserve the real stdout for the protocol; route everything else to stderr.

    Libraries (and the SDK banner) print to stdout; any stray byte would corrupt
    the protocol stream, so fd 1 is pointed at stderr at the OS level.
    """
    proto_fd = os.dup(1)
    os.dup2(2, 1)
    sys.stdout = sys.stderr
    return os.fdopen(proto_fd, "w", buffering=1, encoding="utf-8")


PROTO_OUT = _claim_stdout()
os.environ.setdefault("OPENHANDS_SUPPRESS_BANNER", "1")

import logging  # noqa: E402
import signal  # noqa: E402
import threading  # noqa: E402
import uuid  # noqa: E402
from pathlib import Path  # noqa: E402
from typing import Any  # noqa: E402

from .llmproxy import start_proxy  # noqa: E402
from .rpc import INVALID_PARAMS, Peer, RPCError  # noqa: E402

log = logging.getLogger("bc_openhands")

MAX_TEXT = 2000

# Bump on any incompatible protocol change (renamed/removed methods or fields,
# changed semantics). Additive, optional fields do not bump it. Must match
# ProtocolVersion in internal/agent/openhands/runtime.go (ADR-0004).
PROTOCOL_VERSION = 1


def _trim(s: str, n: int = MAX_TEXT) -> str:
    s = s or ""
    return s if len(s) <= n else s[: n // 2] + f"\n…[{len(s) - n} chars elided]…\n" + s[-n // 2 :]


def summarize_event(ev: Any) -> dict[str, Any]:
    """Compact, runtime-neutral summary of an OpenHands event."""
    kind = type(ev).__name__
    out: dict[str, Any] = {"kind": kind, "id": str(getattr(ev, "id", "")), "source": str(getattr(ev, "source", ""))}
    if kind == "ActionEvent":
        out["tool"] = ev.tool_name
        out["thought"] = _trim(" ".join(t.text for t in (ev.thought or [])), 600)
        if ev.action is not None:
            out["action"] = _trim(ev.action.model_dump_json(exclude_none=True), 1500)
    elif kind == "ObservationEvent":
        out["tool"] = ev.tool_name
        obs = ev.observation
        text = ""
        try:
            content = obs.to_llm_content
            text = "\n".join(getattr(c, "text", "") for c in content)
        except Exception:  # noqa: BLE001 - best effort summary
            text = str(obs)
        out["text"] = _trim(text, 1500)
        out["is_error"] = bool(getattr(obs, "is_error", False))
    elif kind == "AgentErrorEvent":
        out["tool"] = ev.tool_name
        out["error"] = _trim(ev.error, 1500)
    elif kind == "MessageEvent":
        msg = ev.llm_message
        out["role"] = msg.role
        out["text"] = _trim("\n".join(getattr(c, "text", "") for c in msg.content), 3000)
    elif kind in ("Condensation", "CondensationSummaryEvent", "CondensationRequest"):
        summary = getattr(ev, "summary", None)
        if summary:
            out["text"] = _trim(summary, 1500)
        forgotten = getattr(ev, "forgotten_event_ids", None)
        if forgotten is not None:
            out["forgotten"] = len(forgotten)
    elif kind == "ConversationStateUpdateEvent":
        out["key"] = getattr(ev, "key", "")
        out["value"] = _trim(str(getattr(ev, "value", "")), 200)
    return out


class Session:
    """Owns at most one OpenHands Conversation."""

    def __init__(self, peer: Peer):
        self.peer = peer
        self.conv = None
        self.proxy = None
        self.lock = threading.Lock()
        self.running = threading.Lock()

    # session.open: start a new conversation or resume a persisted one.
    def open(self, p: dict[str, Any]) -> dict[str, Any]:
        from openhands.sdk import LLM, Agent, Conversation, Tool
        from openhands.sdk.context.condenser import LLMSummarizingCondenser
        from openhands.tools.file_editor import FileEditorTool
        from openhands.tools.task_tracker import TaskTrackerTool
        from openhands.tools.terminal import TerminalTool

        try:
            workspace = p["workspace"]
            persistence_dir = p["persistence_dir"]
            model = p["model"]
        except KeyError as e:
            raise RPCError(INVALID_PARAMS, f"missing param {e}") from None
        with self.lock:
            if self.conv is not None:
                raise RPCError(INVALID_PARAMS, "a conversation is already open in this adapter")
            if self.proxy is None:
                self.proxy = start_proxy(self.peer, model, timeout=float(p.get("llm_timeout") or 1800))
            port = self.proxy.server_address[1]

            llm = LLM(
                usage_id="agent",
                model=f"openai/{model}",
                base_url=f"http://127.0.0.1:{port}/v1",
                api_key="local-tunnel",  # placeholder; no credential crosses this boundary
                stream=False,
                native_tool_calling=bool(p.get("native_tool_calling", True)),
                num_retries=int(p.get("num_retries") or 2),
                retry_min_wait=2,
                retry_max_wait=10,
                timeout=int(p.get("llm_timeout") or 1800),
                max_input_tokens=p.get("max_input_tokens"),
                max_output_tokens=p.get("max_output_tokens"),
                caching_prompt=False,
                litellm_extra_body=p.get("extra_body") or {},
            )
            condenser = LLMSummarizingCondenser(
                llm=llm.model_copy(update={"usage_id": "condenser"}),
                max_size=int(p.get("condenser_max_events") or 80),
                max_tokens=p.get("condenser_max_tokens"),
                keep_first=int(p.get("condenser_keep_first") or 2),
            )
            tools = [
                Tool(name=TerminalTool.name, params={"terminal_type": "subprocess"}),
                Tool(name=FileEditorTool.name),
                Tool(name=TaskTrackerTool.name),
            ]
            agent = Agent(llm=llm, tools=tools, condenser=condenser)

            conv_id = p.get("conversation_id")
            cid = uuid.UUID(conv_id) if conv_id else uuid.uuid4()
            resumed = (Path(persistence_dir) / cid.hex / "base_state.json").exists()
            self.conv = Conversation(
                agent=agent,
                workspace=workspace,
                persistence_dir=persistence_dir,
                conversation_id=cid,
                callbacks=[self._on_event],
                max_iteration_per_run=int(p.get("max_iterations") or 150),
                stuck_detection=True,
                visualizer=None,
                delete_on_close=False,
            )
            return {
                "conversation_id": str(cid),
                "resumed": resumed,
                "event_count": len(self.conv.state.events),
                "status": self._status(),
            }

    def _on_event(self, ev: Any) -> None:
        try:
            self.peer.notify("event", summarize_event(ev))
        except Exception:  # noqa: BLE001 - never break the agent loop on telemetry
            log.exception("event notify failed")

    def _status(self) -> str:
        st = self.conv.state.execution_status
        return getattr(st, "value", str(st))

    def _require(self):
        if self.conv is None:
            raise RPCError(INVALID_PARAMS, "no open conversation; call session.open first")
        return self.conv

    # session.send: deliver a user message and run until the agent stops.
    def send(self, p: dict[str, Any]) -> dict[str, Any]:
        conv = self._require()
        if not self.running.acquire(blocking=False):
            raise RPCError(INVALID_PARAMS, "conversation is already running")
        try:
            before = len(conv.state.events)
            if p.get("message"):
                conv.send_message(p["message"])
            error = ""
            try:
                conv.run()
            except Exception as e:  # noqa: BLE001 - report, keep adapter alive
                log.exception("conversation run failed")
                error = f"{type(e).__name__}: {e}"
            return self._result(before, error)
        finally:
            self.running.release()

    def _result(self, before: int, error: str = "") -> dict[str, Any]:
        conv = self.conv
        events = list(conv.state.events)
        final = ""
        for ev in reversed(events):
            if type(ev).__name__ == "MessageEvent" and ev.llm_message.role == "assistant":
                final = "\n".join(getattr(c, "text", "") for c in ev.llm_message.content)
                break
            if type(ev).__name__ == "ActionEvent" and ev.tool_name == "finish" and ev.action is not None:
                final = getattr(ev.action, "message", "") or final
                break
        stuck = False
        try:
            stuck = bool(conv.stuck_detector and conv.stuck_detector.is_stuck())
        except Exception:  # noqa: BLE001
            pass
        stats = {}
        try:
            m = conv.conversation_stats.get_combined_metrics()
            tu = m.accumulated_token_usage
            stats = {
                "prompt_tokens": getattr(tu, "prompt_tokens", 0),
                "completion_tokens": getattr(tu, "completion_tokens", 0),
            }
        except Exception:  # noqa: BLE001
            pass
        return {
            "status": self._status(),
            "events_total": len(events),
            "events_new": len(events) - before,
            "final_message": _trim(final, 6000),
            "stuck": stuck,
            "error": error,
            "usage": stats,
        }

    def interrupt(self, _p: dict[str, Any]) -> dict[str, Any]:
        conv = self._require()
        if hasattr(conv, "interrupt"):
            conv.interrupt()
        else:
            conv.pause()
        return {"status": self._status()}

    def state(self, _p: dict[str, Any]) -> dict[str, Any]:
        conv = self._require()
        return {"status": self._status(), "event_count": len(conv.state.events), "conversation_id": str(conv.id)}

    def condense(self, _p: dict[str, Any]) -> dict[str, Any]:
        conv = self._require()
        if not self.running.acquire(blocking=False):
            raise RPCError(INVALID_PARAMS, "conversation is running")
        try:
            before = len(conv.state.events)
            conv.condense()
            return {"events_new": len(conv.state.events) - before, "status": self._status()}
        finally:
            self.running.release()

    def close(self) -> None:
        with self.lock:
            if self.conv is not None:
                try:
                    self.conv.close()
                except Exception:  # noqa: BLE001
                    log.exception("close failed")
                self.conv = None
            if self.proxy is not None:
                self.proxy.shutdown()
                self.proxy = None


def main() -> None:
    logging.basicConfig(
        level=os.environ.get("BC_ADAPTER_LOG", "INFO"),
        stream=sys.stderr,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )
    peer = Peer(sys.stdin, PROTO_OUT)
    session = Session(peer)
    done = threading.Event()

    def shutdown(_p: dict[str, Any]) -> dict[str, Any]:
        session.close()
        # Let the reply reach the peer before the main loop exits.
        threading.Timer(0.2, done.set).start()
        return {"ok": True}

    peer.register("session.open", session.open)
    peer.register("session.send", session.send)
    peer.register("session.interrupt", session.interrupt)
    peer.register("session.state", session.state)
    peer.register("session.condense", session.condense)
    peer.register("shutdown", shutdown)

    reader = threading.Thread(target=peer.serve, name="rpc-reader", daemon=True)
    # SIGTERM (docker stop / kill) ends the loop like EOF so the conversation
    # is closed and persisted cleanly.
    signal.signal(signal.SIGTERM, lambda *_: done.set())
    try:
        reader.start()
        import openhands.sdk  # noqa: F401 - import early so `ready` means the SDK loaded

        peer.notify("ready", ready_params())
        while not done.is_set() and not peer.closed.is_set():
            done.wait(0.5)
    except KeyboardInterrupt:
        log.info("interrupted; closing session")
    finally:
        session.close()


def ready_params() -> dict[str, Any]:
    return {
        "adapter": "bc-openhands",
        "protocol_version": PROTOCOL_VERSION,
        "sdk_version": _sdk_version(),
        "pid": os.getpid(),
    }


def _sdk_version() -> str:
    try:
        from importlib.metadata import version

        return version("openhands-sdk")
    except Exception:  # noqa: BLE001
        return "unknown"


if __name__ == "__main__":
    main()
