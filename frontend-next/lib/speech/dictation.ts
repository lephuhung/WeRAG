/* Live dictation: microphone → 16 kHz PCM → the sherpa-onnx speech service
 * (WS /asr/ws, proxied by next.config / nginx). The service cuts speech at
 * pauses and answers with "partial" text while an utterance is in progress
 * and "final" text once the speaker pauses. */

import { apiGet, authHeaders } from "@/lib/api-client";

export { appendDictated } from "@/lib/speech/append-dictated";

export type DictationError = "insecure" | "unsupported" | "denied" | "no_mic" | "unavailable" | "unauthorized" | "too_long";

export type DictationHandlers = {
  /** End of the draft the dictated text continues; the service punctuates
   *  with it as context. Read when the connection opens. */
  context: () => string;
  /** `transcript` is the whole session so far, punctuated; a partial ends
   *  with its not yet final words. */
  onPartial: (transcript: string) => void;
  onFinal: (transcript: string) => void;
  onError: (e: DictationError) => void;
  /** The session ended (stop() finished, the service closed, or an error). */
  onEnd: () => void;
};

export type Dictation = {
  /** Stop recording and wait for the text still being recognised. */
  stop: () => void;
  /** End at once; text still in flight is dropped. */
  cancel: () => void;
};

const TARGET_RATE = 16000;

// Downmixes to mono, decimates to 16 kHz (box average over each output
// sample's input span, a crude low-pass) and posts ~100 ms Int16 chunks.
// Inline so no extra static file has to be served.
const WORKLET = `
class Pcm16 extends AudioWorkletProcessor {
  constructor() {
    super();
    this.ratio = sampleRate / ${TARGET_RATE};
    this.pos = 0;
    this.acc = 0;
    this.cnt = 0;
    this.out = new Int16Array(${TARGET_RATE / 10});
    this.n = 0;
    // "flush": post the samples of the unfinished chunk, then "flushed".
    this.port.onmessage = () => {
      if (this.n > 0) this.port.postMessage(this.out.slice(0, this.n).buffer);
      this.n = 0;
      this.port.postMessage("flushed");
    };
  }
  process(inputs) {
    const ch = inputs[0];
    if (!ch || ch.length === 0) return true;
    const len = ch[0].length;
    for (let i = 0; i < len; i++) {
      let s = 0;
      for (let c = 0; c < ch.length; c++) s += ch[c][i];
      this.acc += s / ch.length;
      this.cnt++;
      this.pos += 1;
      if (this.pos >= this.ratio) {
        this.pos -= this.ratio;
        const v = this.acc / this.cnt;
        this.acc = 0;
        this.cnt = 0;
        this.out[this.n++] = Math.max(-1, Math.min(1, v)) * 0x7fff;
        if (this.n === this.out.length) {
          this.port.postMessage(this.out.buffer, [this.out.buffer]);
          this.out = new Int16Array(${TARGET_RATE / 10});
          this.n = 0;
        }
      }
    }
    return true;
  }
}
registerProcessor("pcm16", Pcm16);
`;

export function dictationSupported(): boolean {
  return typeof window !== "undefined" && typeof WebSocket !== "undefined" && typeof AudioWorkletNode !== "undefined";
}

export async function startDictation(h: DictationHandlers): Promise<Dictation | null> {
  if (!window.isSecureContext) {
    h.onError("insecure");
    return null;
  }
  if (!dictationSupported() || !navigator.mediaDevices?.getUserMedia) {
    h.onError("unsupported");
    return null;
  }

  let media: MediaStream;
  try {
    media = await navigator.mediaDevices.getUserMedia({
      audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
    });
  } catch (e) {
    const name = (e as DOMException)?.name;
    h.onError(name === "NotAllowedError" || name === "SecurityError" ? "denied" : "no_mic");
    return null;
  }

  // A cheap authenticated call first: it refreshes an expired access token,
  // which the speech service would otherwise reject.
  try {
    await apiGet("/api/v1/auth/me");
  } catch {
    /* the service's own check reports it */
  }
  const headers = authHeaders();
  const token = (headers.Authorization ?? "").replace(/^Bearer /, "");

  let ended = false;
  let stopping = false;
  let ctx: AudioContext | null = null;
  let node: AudioWorkletNode | null = null;
  const ws = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/asr/ws`);
  ws.binaryType = "arraybuffer";

  const fail = (e: DictationError) => {
    if (!ended) h.onError(e);
    cleanup();
  };

  const cleanup = () => {
    if (ended) return;
    ended = true;
    if (node) {
      node.port.onmessage = null;
      node.port.close();
      node.disconnect();
    }
    media.getTracks().forEach((t) => t.stop());
    void ctx?.close();
    if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) ws.close();
    h.onEnd();
  };

  const sendStop = () => {
    if (ended || ws.readyState !== WebSocket.OPEN) return cleanup();
    ws.send(JSON.stringify({ type: "stop" }));
    // The service answers "done" once the last utterance is decoded.
    setTimeout(cleanup, 15000);
  };

  ws.onopen = () =>
    ws.send(JSON.stringify({ token, tenant_id: headers["X-Tenant-ID"] ?? "", context: h.context().slice(-500) }));
  ws.onerror = () => fail("unavailable");
  // A close the service did not announce with "done"/"error" is a failure.
  ws.onclose = () => fail("unavailable");
  ws.onmessage = async (ev) => {
    let msg: { type: string; text?: string; transcript?: string; code?: string };
    try {
      msg = JSON.parse(String(ev.data));
    } catch {
      return;
    }
    if (msg.type === "ready") {
      try {
        const c = new AudioContext();
        ctx = c;
        const url = URL.createObjectURL(new Blob([WORKLET], { type: "application/javascript" }));
        await c.audioWorklet.addModule(url);
        URL.revokeObjectURL(url);
        if (ended || stopping) return;
        const n = new AudioWorkletNode(c, "pcm16");
        node = n;
        n.port.onmessage = (e) => {
          if (e.data === "flushed") sendStop();
          else if (ws.readyState === WebSocket.OPEN) ws.send(e.data as ArrayBuffer);
        };
        c.createMediaStreamSource(media).connect(n);
      } catch {
        fail("unsupported");
      }
    } else if (msg.type === "partial") h.onPartial(msg.transcript ?? "");
    else if (msg.type === "final") h.onFinal(msg.transcript ?? "");
    else if (msg.type === "done") cleanup();
    else if (msg.type === "error") {
      fail(msg.code === "unauthorized" ? "unauthorized" : msg.code === "too_long" ? "too_long" : "unavailable");
    }
  };

  return {
    stop: () => {
      if (ended || stopping) return;
      stopping = true;
      // Stop capturing, send the audio still in the worklet, then let the
      // service decode what it holds.
      media.getTracks().forEach((t) => t.stop());
      if (node) {
        node.port.postMessage("flush");
        setTimeout(() => !ended && sendStop(), 1000); // worklet never answered
      } else sendStop();
    },
    cancel: cleanup,
  };
}
