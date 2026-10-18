"""Vietnamese speech-to-text for WeRAG, built on sherpa-onnx (CPU).

Two entry points:

* ``WS /ws`` — live dictation from the browser. The first frame is JSON
  ``{"token": "...", "tenant_id": "..."}`` (checked against the WeRAG backend's
  ``/api/v1/auth/me``); after that every binary frame is 16 kHz mono 16-bit
  little-endian PCM. Silero VAD cuts the stream into utterances; while one is
  in progress the server re-decodes it every ``PARTIAL_EVERY`` seconds and
  sends ``{"type": "partial", "text"}``, and when the speaker pauses it sends
  ``{"type": "final", "text"}``. Both carry ``transcript``: the whole session
  so far, punctuated and capitalised by vibert-capu (the hello may pass
  ``context``, the end of the user's draft, so a continued sentence is not
  capitalised or closed early); a partial's not yet final words are appended
  raw.
* ``POST /v1/audio/transcriptions`` — OpenAI-compatible file transcription, so
  the existing ASR model slot (KB audio files, chat audio attachments) can use
  the same model. Guarded by ``ASR_API_KEY`` when it is set.
"""

import asyncio
import json
import logging
import os
import subprocess
import time

import httpx
import numpy as np
import sherpa_onnx as so
from capu import Capu, Transcript
from fastapi import FastAPI, File, Form, Header, HTTPException, UploadFile, WebSocket, WebSocketDisconnect

MODEL_DIR = os.environ.get("ASR_MODEL_DIR", "/models/sherpa-onnx-zipformer-vi-30M-int8-2026-02-09")
VAD_MODEL = os.environ.get("ASR_VAD_MODEL", "/models/silero_vad.onnx")
BACKEND_URL = os.environ.get("WERAG_BACKEND_URL", "http://10.10.0.241:18080").rstrip("/")
API_KEY = os.environ.get("ASR_API_KEY", "")
THREADS = int(os.environ.get("ASR_THREADS", "4"))
MAX_DECODES = int(os.environ.get("ASR_MAX_PARALLEL", "4"))
MAX_SESSION_SEC = int(os.environ.get("ASR_MAX_SESSION_SEC", "600"))
MAX_FILE_BYTES = int(os.environ.get("ASR_MAX_FILE_MB", "100")) * 1024 * 1024
MODEL_NAME = "zipformer-vi-30M"
PUNCTUATE = os.environ.get("ASR_PUNCTUATE", "1") == "1"

SR = 16000
WINDOW = 512  # silero VAD window at 16 kHz
PARTIAL_EVERY = 0.35
AUTH_CACHE_SEC = 60

log = logging.getLogger("sherpa-asr")
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

recognizer = so.OfflineRecognizer.from_transducer(
    encoder=f"{MODEL_DIR}/encoder.int8.onnx",
    decoder=f"{MODEL_DIR}/decoder.onnx",
    joiner=f"{MODEL_DIR}/joiner.int8.onnx",
    tokens=f"{MODEL_DIR}/tokens.txt",
    num_threads=THREADS,
)
capu = Capu() if PUNCTUATE else None
decode_slots = asyncio.Semaphore(MAX_DECODES)
auth_cache: dict[str, float] = {}

app = FastAPI(title="sherpa-asr")


def new_vad(threshold: float = 0.5, min_silence: float = 0.5) -> so.VoiceActivityDetector:
    cfg = so.VadModelConfig()
    cfg.silero_vad.model = VAD_MODEL
    cfg.silero_vad.threshold = threshold
    cfg.silero_vad.min_silence_duration = min_silence
    cfg.silero_vad.min_speech_duration = 0.25
    cfg.silero_vad.max_speech_duration = 20
    cfg.silero_vad.window_size = WINDOW
    cfg.sample_rate = SR
    return so.VoiceActivityDetector(cfg, buffer_size_in_seconds=60)


def decode(samples: np.ndarray) -> str:
    """Lower-case text of one utterance (the model emits upper case, no
    punctuation). Live dictation leaves capitalisation to the client, which
    knows whether the utterance starts a sentence."""
    s = recognizer.create_stream()
    s.accept_waveform(SR, samples)
    recognizer.decode_stream(s)
    return " ".join(s.result.text.split()).lower()


def tidy(text: str) -> str:
    """Capitalise the first letter of a transcript."""
    return text[:1].upper() + text[1:]


async def decode_async(samples: np.ndarray) -> str:
    async with decode_slots:
        return await asyncio.to_thread(decode, samples)


async def check_token(token: str, tenant_id: str) -> str:
    """"ok", "unauthorized", or "unavailable" (backend not reachable)."""
    key = f"{tenant_id}:{token}"
    now = time.time()
    if auth_cache.get(key, 0) > now:
        return "ok"
    headers = {"Authorization": f"Bearer {token}"}
    if tenant_id:
        headers["X-Tenant-ID"] = tenant_id
    try:
        async with httpx.AsyncClient(timeout=5) as c:
            r = await c.get(f"{BACKEND_URL}/api/v1/auth/me", headers=headers)
    except httpx.HTTPError as e:
        log.warning("auth check failed: %s", e)
        return "unavailable"
    if r.status_code in (401, 403):
        return "unauthorized"
    if r.status_code != 200:
        return "unavailable"
    for k in [k for k, exp in auth_cache.items() if exp <= now]:
        del auth_cache[k]
    auth_cache[key] = now + AUTH_CACHE_SEC
    return "ok"


@app.get("/health")
def health():
    return {"status": "ok", "model": MODEL_NAME}


@app.websocket("/ws")
async def dictate(ws: WebSocket):
    await ws.accept()
    try:
        hello = json.loads(await asyncio.wait_for(ws.receive_text(), timeout=10))
        if not isinstance(hello, dict):
            raise ValueError("hello must be an object")
    except (asyncio.TimeoutError, ValueError, WebSocketDisconnect):
        await ws.close(code=4400)
        return
    auth = await check_token(str(hello.get("token", "")), str(hello.get("tenant_id", "")))
    if auth != "ok":
        await ws.send_json({"type": "error", "code": auth})
        await ws.close(code=4401)
        return
    await ws.send_json({"type": "ready", "model": MODEL_NAME, "punctuate": capu is not None})
    transcript = Transcript(capu, str(hello.get("context", ""))[-500:]) if capu else None
    raw_finals: list[str] = []

    def session_text(pending: str = "") -> str:
        done = transcript.text() if transcript else " ".join(raw_finals)
        if pending and transcript and (not done or done.endswith((".", "?"))):
            pending = pending[:1].upper() + pending[1:]
        return f"{done} {pending}".strip()

    vad = new_vad()
    buf = np.zeros(0, dtype=np.float32)  # audio since the current utterance began (plus lead-in)
    offset = 0  # how much of buf the VAD has already seen
    speaking = False
    utterance = 0  # bumped on every final; a partial of an older utterance is dropped
    partial_task: asyncio.Task | None = None
    last_partial = 0.0
    received = 0

    async def send_partial(samples: np.ndarray, of: int):
        nonlocal last_partial
        text = await decode_async(samples)
        # Measured from the end of the decode, so slow decodes space themselves out.
        last_partial = time.monotonic()
        if text and of == utterance:
            await ws.send_json({"type": "partial", "text": text, "transcript": session_text(text)})

    async def send_finals():
        nonlocal utterance
        while not vad.empty():
            samples = np.asarray(vad.front.samples, dtype=np.float32)
            vad.pop()
            text = await decode_async(samples)
            utterance += 1
            if not text:
                continue
            if transcript:
                async with decode_slots:
                    await asyncio.to_thread(transcript.add, text)
            else:
                raw_finals.append(text)
            await ws.send_json({"type": "final", "text": text, "transcript": session_text()})

    try:
        while True:
            msg = await ws.receive()
            if msg["type"] == "websocket.disconnect":
                break
            if msg.get("text") is not None:
                try:
                    stop = json.loads(msg["text"]).get("type") == "stop"
                except (ValueError, AttributeError):
                    stop = False
                if not stop:
                    continue
                vad.flush()
                await send_finals()
                utterance += 1  # a partial still decoding must not follow "done"
                await ws.send_json({"type": "done"})
                vad.reset()
                buf, offset, speaking = np.zeros(0, dtype=np.float32), 0, False
                continue
            pcm = np.frombuffer(msg["bytes"][: len(msg["bytes"]) // 2 * 2], dtype="<i2").astype(np.float32) / 32768.0
            received += len(pcm)
            if received > MAX_SESSION_SEC * SR:
                await ws.send_json({"type": "error", "code": "too_long"})
                await ws.close(code=1000)
                break
            buf = np.concatenate([buf, pcm])
            while offset + WINDOW <= len(buf):
                vad.accept_waveform(buf[offset : offset + WINDOW])
                if not speaking and vad.is_speech_detected():
                    speaking = True
                    last_partial = time.monotonic()
                offset += WINDOW
            if not vad.empty():
                await send_finals()
                buf, offset, speaking = np.zeros(0, dtype=np.float32), 0, False
                continue
            if not speaking and len(buf) > 10 * WINDOW:
                # Keep a short lead-in only; silence before speech is useless.
                drop = len(buf) - 10 * WINDOW
                buf, offset = buf[drop:], offset - drop
            # At most one partial decode in flight per session; audio keeps
            # flowing into the VAD meanwhile.
            if (speaking and (partial_task is None or partial_task.done())
                    and time.monotonic() - last_partial > PARTIAL_EVERY):
                partial_task = asyncio.create_task(send_partial(buf.copy(), utterance))
    except WebSocketDisconnect:
        pass
    finally:
        if partial_task and not partial_task.done():
            partial_task.cancel()
        log.info("dictation session closed after %.1f s of audio", received / SR)


def load_audio(data: bytes) -> np.ndarray:
    p = subprocess.run(
        ["ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-i", "pipe:0",
         "-ac", "1", "-ar", str(SR), "-f", "s16le", "pipe:1"],
        input=data, capture_output=True, timeout=300,
    )
    if p.returncode != 0:
        raise HTTPException(400, f"cannot decode audio: {p.stderr.decode(errors='replace')[:200]}")
    return np.frombuffer(p.stdout, dtype="<i2").astype(np.float32) / 32768.0


def split_speech(samples: np.ndarray) -> list[tuple[int, np.ndarray]]:
    """(start sample, audio) of each utterance in a whole file. Files are often
    low-bitrate recordings: a lower threshold keeps quiet sentence starts, and
    longer pauses give the model whole sentences."""
    vad = new_vad(threshold=0.3, min_silence=0.8)
    segments = []

    def drain():
        while not vad.empty():
            segments.append((vad.front.start, np.asarray(vad.front.samples, dtype=np.float32)))
            vad.pop()

    for i in range(0, len(samples) - WINDOW + 1, WINDOW):
        vad.accept_waveform(samples[i : i + WINDOW])
        drain()
    vad.flush()
    drain()
    return segments


@app.post("/v1/audio/transcriptions")
async def transcribe(
    file: UploadFile = File(...),
    model: str = Form(MODEL_NAME),
    response_format: str = Form("json"),
    authorization: str = Header(""),
):
    if not API_KEY:
        raise HTTPException(503, "file transcription is disabled: ASR_API_KEY is not set")
    if authorization != f"Bearer {API_KEY}":
        raise HTTPException(401, "invalid api key")
    data = await file.read(MAX_FILE_BYTES + 1)
    if len(data) > MAX_FILE_BYTES:
        raise HTTPException(413, "audio file too large")
    samples = await asyncio.to_thread(load_audio, data)

    segments = await asyncio.to_thread(split_speech, samples)

    out = []
    for start, seg in segments:
        text = await decode_async(seg)
        if text:
            out.append({"start": start / SR, "end": (start + len(seg)) / SR, "text": text})
    full = " ".join(s["text"] for s in out)
    full = await asyncio.to_thread(capu.restore, full) if capu and full else tidy(full)
    if response_format == "text":
        return full
    if response_format == "verbose_json":
        return {"text": full, "language": "vi", "duration": len(samples) / SR, "segments": out}
    return {"text": full}
