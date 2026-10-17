"""Thin HTTP client for driving the WeRAG document assistant like the web UI.

Covers: sessions, attachments, document tabs (target/source), the agent-chat
SSE stream, format-check polling and cleanup. No third-party dependency
besides `requests`.
"""
from __future__ import annotations

import json
import os
import time
import uuid
from dataclasses import dataclass, field
from typing import Any, Dict, Iterable, List, Optional

import requests

DOCUMENT_ASSISTANT = "builtin-document-assistant"


class ApiError(RuntimeError):
    def __init__(self, status: int, body: str, where: str):
        super().__init__(f"{where}: HTTP {status}: {body[:400]}")
        self.status = status
        self.body = body


@dataclass
class ToolCall:
    call_id: str
    name: str
    arguments: Dict[str, Any] = field(default_factory=dict)
    result: Optional[Dict[str, Any]] = None  # tool_result event data

    @property
    def success(self) -> Optional[bool]:
        if self.result is None:
            return None
        return bool(self.result.get("success"))

    @property
    def output(self) -> str:
        if not self.result:
            return ""
        return str(self.result.get("output") or self.result.get("content") or "")

    @property
    def data(self) -> Dict[str, Any]:
        return self.result or {}

    @property
    def document_ops(self) -> List[Dict[str, Any]]:
        ops = self.data.get("document_ops")
        return ops if isinstance(ops, list) else []


@dataclass
class Turn:
    query: str
    request: Dict[str, Any]
    answer: str = ""
    final_content: str = ""
    thinking: str = ""
    tool_calls: List[ToolCall] = field(default_factory=list)
    events: List[Dict[str, Any]] = field(default_factory=list)
    error: Optional[str] = None
    assistant_message_id: str = ""
    duration_s: float = 0.0
    http_status: int = 0

    # ---- convenience -----------------------------------------------------
    @property
    def text(self) -> str:
        """Best final answer text (saved content wins over streamed chunks)."""
        return (self.final_content or self.answer or "").strip()

    def tools(self, name: Optional[str] = None) -> List[ToolCall]:
        return [t for t in self.tool_calls if name is None or t.name == name]

    def tool_names(self) -> List[str]:
        return [t.name for t in self.tool_calls]

    def called(self, name: str) -> bool:
        return any(t.name == name for t in self.tool_calls)

    def first_tool(self) -> Optional[str]:
        return self.tool_calls[0].name if self.tool_calls else None

    def all_ops(self) -> List[Dict[str, Any]]:
        out: List[Dict[str, Any]] = []
        for t in self.tool_calls:
            out.extend(t.document_ops)
        return out

    def ops(self, kind: str) -> List[Dict[str, Any]]:
        return [o for o in self.all_ops() if o.get("op") == kind]

    def display_types(self) -> List[str]:
        out = []
        for t in self.tool_calls:
            dt = t.data.get("display_type")
            if dt:
                out.append(dt)
        return out

    def proposals(self) -> List[ToolCall]:
        return [t for t in self.tool_calls if t.data.get("proposal") is True]

    def summary(self) -> Dict[str, Any]:
        return {
            "query": self.query,
            "answer": self.text,
            "error": self.error,
            "duration_s": round(self.duration_s, 1),
            "tools": [
                {
                    "name": t.name,
                    "arguments": t.arguments,
                    "success": t.success,
                    "error": (t.result or {}).get("error"),
                    "display_type": t.data.get("display_type"),
                    "ops": [o.get("op") for o in t.document_ops],
                    "output": t.output[:1500],
                    "data_keys": sorted(k for k in t.data.keys()),
                }
                for t in self.tool_calls
            ],
            "display_types": self.display_types(),
        }


class WeragClient:
    def __init__(self, base_url: str, token: str, tenant_id: Optional[int] = None,
                 timeout: float = 600.0, verbose: bool = False):
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.tenant_id = tenant_id
        self.timeout = timeout
        self.verbose = verbose
        self.s = requests.Session()

    # ---- plumbing ----------------------------------------------------------
    def _headers(self, extra: Optional[Dict[str, str]] = None) -> Dict[str, str]:
        h = {"Authorization": f"Bearer {self.token}", "X-Request-ID": uuid.uuid4().hex}
        # Eval traffic must never be distilled into the account's long-term memory.
        h["X-WeRAG-Memory"] = "off"
        if self.tenant_id:
            h["X-Tenant-ID"] = str(self.tenant_id)
        if extra:
            h.update(extra)
        return h

    def _req(self, method: str, path: str, where: str = "", **kw) -> Any:
        url = self.base_url + path
        kw.setdefault("timeout", 60)
        headers = self._headers(kw.pop("headers", None))
        # a dev backend restarts on hot reload: retry transport errors a few times
        for attempt in range(4):
            try:
                r = self.s.request(method, url, headers=headers, **kw)
                break
            except requests.ConnectionError as e:
                if attempt == 3:
                    raise
                if self.verbose:
                    print(f"  [retry] {where or path}: {e}")
                time.sleep(5 * (attempt + 1))
        if r.status_code >= 300:
            raise ApiError(r.status_code, r.text, where or f"{method} {path}")
        if not r.content:
            return None
        try:
            return r.json()
        except ValueError:
            return r.text

    # ---- auth ------------------------------------------------------------
    @staticmethod
    def login(base_url: str, email: str, password: str, two_factor_code: str = "") -> Dict[str, Any]:
        body = {"email": email, "password": password}
        if two_factor_code:
            body["two_factor_code"] = two_factor_code
        r = requests.post(base_url.rstrip("/") + "/api/v1/auth/login", json=body, timeout=30)
        if r.status_code >= 300:
            raise ApiError(r.status_code, r.text, "login")
        return r.json()

    def me(self) -> Any:
        return self._req("GET", "/api/v1/auth/me", "me")

    # ---- sessions --------------------------------------------------------
    def create_session(self, title: str = "") -> str:
        data = self._req("POST", "/api/v1/sessions", "create_session", json={"title": title, "description": ""})
        d = data.get("data") if isinstance(data, dict) else None
        sid = (d or {}).get("id") if isinstance(d, dict) else None
        if not sid:
            raise RuntimeError(f"create_session: unexpected body {json.dumps(data)[:300]}")
        return sid

    def delete_session(self, session_id: str) -> None:
        try:
            self._req("DELETE", f"/api/v1/sessions/{session_id}", "delete_session")
        except ApiError as e:
            if e.status != 404:
                raise

    def messages(self, session_id: str, limit: int = 50) -> Any:
        return self._req("GET", f"/api/v1/messages/{session_id}/load?limit={limit}", "messages")

    # ---- attachments / documents ----------------------------------------
    def upload_attachment(self, session_id: str, path: str, role: str = "target",
                          agent_id: str = DOCUMENT_ASSISTANT) -> Dict[str, Any]:
        """Upload a file. role="target" → later opened as a tab; role="source" → source document."""
        data = {"agent_id": agent_id, "parser_engine": "auto", "document_role": role}
        for attempt in range(4):
            with open(path, "rb") as f:
                files = {"file": (os.path.basename(path), f,
                                  "application/vnd.openxmlformats-officedocument.wordprocessingml.document")}
                try:
                    r = self.s.post(self.base_url + f"/api/v1/sessions/{session_id}/attachments",
                                    headers=self._headers(), files=files, data=data, timeout=120)
                    break
                except requests.ConnectionError:
                    if attempt == 3:
                        raise
                    time.sleep(5 * (attempt + 1))
        if r.status_code >= 300:
            raise ApiError(r.status_code, r.text, "upload_attachment")
        return r.json()

    def open_document(self, session_id: str, attachment_id: str, editor_kind: str = "word_addin") -> Dict[str, Any]:
        data = self._req("POST", f"/api/v1/sessions/{session_id}/documents", "open_document",
                         json={"attachment_id": attachment_id, "editor_kind": editor_kind})
        return data.get("data", data)

    def list_documents(self, session_id: str) -> Dict[str, Any]:
        data = self._req("GET", f"/api/v1/sessions/{session_id}/documents", "list_documents")
        return data.get("data", data) if isinstance(data, dict) else data

    def get_attachment(self, session_id: str, attachment_id: str) -> Dict[str, Any]:
        data = self._req("GET", f"/api/v1/sessions/{session_id}/attachments/{attachment_id}", "get_attachment")
        return data.get("data", data) if isinstance(data, dict) else data

    def wait_attachment_ready(self, session_id: str, attachment_id: str, timeout: float = 180) -> Dict[str, Any]:
        t0 = time.time()
        last: Dict[str, Any] = {}
        while time.time() - t0 < timeout:
            last = self.get_attachment(session_id, attachment_id)
            st = last.get("status")
            if st == "ready":
                return last
            if st == "failed":
                raise RuntimeError(f"attachment {attachment_id} failed: {json.dumps(last)[:300]}")
            time.sleep(2)
        raise TimeoutError(f"attachment {attachment_id} not ready after {timeout}s: {last.get('status')}")

    def wait_source_text(self, session_id: str, document_id: str, timeout: float = 180) -> Dict[str, Any]:
        t0 = time.time()
        while time.time() - t0 < timeout:
            docs = self.list_documents(session_id).get("documents") or []
            for d in docs:
                if d.get("id") == document_id:
                    ts = d.get("text_status", "")
                    if ts in ("", "ready"):
                        return d
                    if ts == "failed":
                        raise RuntimeError(f"source {document_id} text failed")
            time.sleep(2)
        raise TimeoutError(f"source {document_id} text not ready after {timeout}s")

    def add_target(self, session_id: str, path: str, editor_kind: str = "word_addin") -> Dict[str, Any]:
        """Upload + open as an editable tab. Returns the DocumentWorkspaceView."""
        up = self.upload_attachment(session_id, path, role="target")
        att = up.get("data") or {}
        att_id = att.get("id")
        if not att_id:
            raise RuntimeError(f"upload returned no attachment id: {json.dumps(up)[:300]}")
        view = self.open_document(session_id, att_id, editor_kind=editor_kind)
        view["attachment_id"] = att_id
        return view

    def add_source(self, session_id: str, path: str) -> Dict[str, Any]:
        """Upload as a chat attachment that becomes a source document. Returns the DocumentWorkspaceView."""
        up = self.upload_attachment(session_id, path, role="source")
        att = up.get("data") or {}
        doc = up.get("document")
        if not doc:
            raise RuntimeError(f"upload did not become a source document: {json.dumps(up)[:300]}")
        if att.get("id"):
            try:
                self.wait_attachment_ready(session_id, att["id"])
            except (RuntimeError, TimeoutError) as e:  # keep going; text may still parse
                if self.verbose:
                    print("  [warn]", e)
        doc = self.wait_source_text(session_id, doc["id"])
        doc["attachment_id"] = att.get("id")
        return doc

    # ---- format check ----------------------------------------------------
    def start_format_check(self, session_id: str, document_id: str) -> Any:
        return self._req("POST", f"/api/v1/sessions/{session_id}/documents/{document_id}/format-check", "start_format_check")

    def get_format_check(self, session_id: str, document_id: str) -> Dict[str, Any]:
        data = self._req("GET", f"/api/v1/sessions/{session_id}/documents/{document_id}/format-check", "get_format_check")
        return data.get("data", data) if isinstance(data, dict) else data

    def wait_format_check(self, session_id: str, document_id: str, timeout: float = 420) -> Dict[str, Any]:
        t0 = time.time()
        last: Dict[str, Any] = {}
        while time.time() - t0 < timeout:
            last = self.get_format_check(session_id, document_id)
            chk = last.get("check") or {}
            st = chk.get("status") or chk.get("state")
            if st in ("ready", "done", "failed", "error") or last.get("report"):
                return last
            time.sleep(5)
        return last

    # ---- chat (SSE) ------------------------------------------------------
    def chat(self, session_id: str, query: str, *, selection: Optional[Dict[str, Any]] = None,
             mentions: Optional[List[Dict[str, Any]]] = None, attachment_ids: Optional[List[str]] = None,
             web_search: bool = False, agent_id: str = DOCUMENT_ASSISTANT, extra: Optional[Dict[str, Any]] = None,
             on_event=None) -> Turn:
        body: Dict[str, Any] = {
            "query": query,
            "agent_enabled": True,
            "agent_id": agent_id,
            "web_search_enabled": web_search,
            "channel": "web",
        }
        if selection:
            body["document_selection"] = selection
        if mentions:
            body["mentioned_items"] = mentions
        if attachment_ids:
            body["attachment_ids"] = attachment_ids
        if extra:
            body.update(extra)
        turn = Turn(query=query, request=body)
        t0 = time.time()
        headers = self._headers({"Accept": "text/event-stream", "Content-Type": "application/json"})
        try:
            with self.s.post(self.base_url + f"/api/v1/agent-chat/{session_id}", headers=headers,
                             data=json.dumps(body), stream=True, timeout=(30, self.timeout)) as r:
                turn.http_status = r.status_code
                if r.status_code >= 300:
                    turn.error = f"HTTP {r.status_code}: {r.text[:500]}"
                    return turn
                self._consume_sse(r, turn, on_event)
        except requests.RequestException as e:
            turn.error = f"transport: {e}"
        turn.duration_s = time.time() - t0
        return turn

    def _consume_sse(self, r: requests.Response, turn: Turn, on_event) -> None:
        calls: Dict[str, ToolCall] = {}
        data_buf: List[str] = []
        for raw in r.iter_lines(decode_unicode=True):
            if raw is None:
                continue
            line = raw if isinstance(raw, str) else raw.decode("utf-8", "replace")
            if line == "":
                if data_buf:
                    payload = "\n".join(data_buf)
                    data_buf = []
                    if payload.strip() == "[DONE]":
                        return
                    try:
                        ev = json.loads(payload)
                    except ValueError:
                        continue
                    done = self._handle_event(ev, turn, calls, on_event)
                    if done:
                        return
                continue
            if line.startswith(":"):
                continue
            if line.startswith("data:"):
                data_buf.append(line[5:].lstrip())
            # "event:" and "id:" lines are ignored

    def _handle_event(self, ev: Dict[str, Any], turn: Turn, calls: Dict[str, ToolCall], on_event) -> bool:
        turn.events.append(ev)
        if on_event:
            on_event(ev)
        rt = ev.get("response_type") or ev.get("type")
        data = ev.get("data") or {}
        content = ev.get("content") or ""
        if rt == "agent_query":
            turn.assistant_message_id = ev.get("assistant_message_id") or data.get("assistant_message_id", "")
        elif rt == "answer":
            if content:
                turn.answer += content
        elif rt == "thinking":
            if content:
                turn.thinking += content
        elif rt == "tool_call":
            cid = data.get("tool_call_id") or f"anon-{len(calls)}"
            name = data.get("tool_name") or ""
            args = data.get("arguments")
            if isinstance(args, str):
                try:
                    args = json.loads(args)
                except ValueError:
                    args = {"_raw": args}
            tc = calls.get(cid)
            if tc is None:
                tc = ToolCall(call_id=cid, name=name)
                calls[cid] = tc
                turn.tool_calls.append(tc)
            if name:
                tc.name = name
            if isinstance(args, dict) and args:
                tc.arguments = args  # the last tool_call event carries the complete arguments
        elif rt == "tool_result":
            cid = data.get("tool_call_id") or ""
            tc = calls.get(cid)
            if tc is None:
                tc = ToolCall(call_id=cid or f"result-{len(calls)}", name=data.get("tool_name") or "")
                calls[tc.call_id] = tc
                turn.tool_calls.append(tc)
            res = dict(data)
            if "output" not in res and content:
                res["content"] = content
            tc.result = res
        elif rt == "error":
            turn.error = content or json.dumps(data)[:300]
        elif rt == "complete":
            fc = data.get("final_content")
            if isinstance(fc, str) and fc.strip():
                turn.final_content = fc
            return True
        elif rt == "stop":
            return True
        return False


def mention(doc: Dict[str, Any]) -> Dict[str, Any]:
    """Build the @-mention item for a document workspace view."""
    return {"id": doc["id"], "name": doc.get("file_name", ""), "type": "document"}


def selection(doc: Dict[str, Any], text: str, paragraph_hint: str = "", occurrence: int = 0) -> Dict[str, Any]:
    sel: Dict[str, Any] = {"text": text, "document_id": doc["id"]}
    if paragraph_hint:
        sel["paragraph_hint"] = paragraph_hint
    if occurrence:
        sel["paragraph_occurrence"] = occurrence
    return sel
