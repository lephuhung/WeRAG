/*
 * WeRAG Assistant — background ONLYOFFICE plugin.
 *
 * With `initDataType: "text"` + `initOnSelectionChanged: true`, the editor
 * calls `init(text)` with the current selection every time it changes. We
 * debounce, drop empty selections, and forward the text to the WeRAG page
 * that embeds the editor (window.top) via postMessage:
 *
 *   { source: "werag-onlyoffice", type: "selection", text: "<selected text>" }
 *
 * The WeRAG frontend only accepts these messages when event.origin equals the
 * Document Server origin.
 *
 * Target origin: WeRAG passes its own origin in the editor config
 * (editorConfig.plugins.options["asc.{7a4b3c2d-...}"].hostOrigin, readable as
 * window.Asc.plugin.info.options.hostOrigin). If that is missing we try
 * location.ancestorOrigins (Chromium/WebKit; last entry = top window). Only
 * when neither is available do we fall back to "*": the payload is just the
 * user's own selection, sent to the page that is already displaying it.
 */
(function (window) {
  "use strict";

  var DEBOUNCE_MS = 120;
  var timer = null;
  var lastSent = "";

  function hostOrigin() {
    try {
      var info = window.Asc && window.Asc.plugin && window.Asc.plugin.info;
      var opts = info && info.options;
      if (opts && typeof opts.hostOrigin === "string" && opts.hostOrigin) {
        return opts.hostOrigin;
      }
    } catch (e) { /* ignore */ }
    try {
      var anc = window.location.ancestorOrigins;
      if (anc && anc.length > 0) return anc[anc.length - 1];
    } catch (e) { /* ignore */ }
    return "*"; // documented fallback, see header
  }

  function post(text) {
    try {
      window.top.postMessage(
        { source: "werag-onlyoffice", type: "selection", text: text },
        hostOrigin()
      );
    } catch (e) { /* host gone or origin mismatch */ }
  }

  function consider(text) {
    var value = typeof text === "string" ? text.trim() : "";
    window.__weragLastSelection = value;
    if (timer) clearTimeout(timer);
    if (!value) {
      lastSent = ""; // re-selecting the same passage later posts it again
      return;
    }
    timer = setTimeout(function () {
      timer = null;
      if (value === lastSent) return;
      lastSent = value;
      post(value);
    }, DEBOUNCE_MS);
  }

  // The editor calls init(text) on every selection change (initOnSelectionChanged),
  // but that callback has proved unreliable right after load and while a review
  // balloon owns the focus, so the current selection is also polled.
  window.Asc.plugin.init = function (text) {
    consider(text);
  };

  var POLL_MS = 400;
  function poll() {
    try {
      window.Asc.plugin.executeMethod("GetSelectedText", [{ Numbering: false, Math: false }], function (text) {
        consider(text);
      });
    } catch (e) { /* editor not ready yet */ }
  }
  setInterval(poll, POLL_MS);

  // Background plugin: no buttons/window, but the API expects a handler.
  window.Asc.plugin.button = function () {};

  /* ===================== AI edit plans (apply_ops) =====================
   *
   * The WeRAG page posts {source:"werag-host", type:"apply_ops", batchId, ops}
   * to the editor iframe (DocsAPI's "frameEditor"). This plugin's iframe is
   * created by the editor inside that frame and is same-origin with it, so
   * we listen on our same-origin ancestors (normally just window.parent; the
   * walk stops at the first cross-origin parent, i.e. the WeRAG page).
   *
   * Replies go to window.top (the WeRAG page):
   *   {source:"werag-onlyoffice", type:"ops_ack", batchId}            on receipt
   *   {source:"werag-onlyoffice", type:"ops_result", batchId, applied, failed:[{index,error}]}
   *
   * The whole batch runs in ONE Asc.plugin.callCommand(func, isClose=false,
   * isCalc=true, callback); data goes through Asc.scope because the command
   * is serialised and runs in its own context (no closures). Every op has
   * its own try/catch. The anchor normaliser mirrors normalizeAnchorText in
   * frontend-next/lib/api/document-ops.ts — keep them in sync.
   */
  var doneBatches = {}; // batchId -> last result (dedupe re-posts)
  var queue = [];
  var running = false;

  function reply(msg) {
    msg.source = "werag-onlyoffice";
    try {
      window.top.postMessage(msg, hostOrigin());
    } catch (e) { /* host gone */ }
  }

  function originAllowed(origin) {
    var want = hostOrigin();
    return want === "*" || origin === want;
  }

  function runNext() {
    if (running || queue.length === 0) return;
    running = true;
    var job = queue.shift();
    window.Asc.scope.weragOps = job.ops;
    var finish = function (raw) {
      var res = null;
      try { res = typeof raw === "string" ? JSON.parse(raw) : raw; } catch (e) { res = null; }
      if (!res || typeof res.applied !== "number") {
        res = { applied: 0, failed: [{ index: -1, error: "command failed" }] };
      }
      var out = { type: "ops_result", batchId: job.batchId, applied: res.applied, failed: res.failed || [] };
      doneBatches[job.batchId] = out;
      if (window.__weragLastBatch && window.__weragLastBatch.batchId === job.batchId) {
        window.__weragLastBatch.result = out;
      } else {
        window.__weragLastBatch = { batchId: job.batchId, ops: job.ops, result: out };
      }
      reply(out);
      running = false;
      runNext();
    };
    try {
      window.Asc.plugin.callCommand(applyOpsCommand, false, true, finish);
    } catch (e) {
      finish(null);
    }
  }

  function onHostMessage(event) {
    var d = event && event.data;
    if (!d || typeof d !== "object" || d.source !== "werag-host" || d.type !== "apply_ops") return;
    // Edit plans need a known host: never act on "*" (selection posting
    // keeps that fallback, applying edits does not).
    if (hostOrigin() === "*") return;
    if (!originAllowed(event.origin)) return;
    if (typeof d.batchId !== "string" || !d.batchId || !Array.isArray(d.ops)) return;
    reply({ type: "ops_ack", batchId: d.batchId });
    if (doneBatches[d.batchId]) {
      if (doneBatches[d.batchId] !== true) reply(doneBatches[d.batchId]);
      return; // never apply the same batch twice
    }
    doneBatches[d.batchId] = true; // queued / running
    window.__weragLastBatch = { batchId: d.batchId, ops: d.ops, result: null }; // diagnostics
    queue.push({ batchId: d.batchId, ops: d.ops });
    runNext();
  }

  (function listen() {
    var targets = [];
    var w = window;
    try {
      while (w.parent && w.parent !== w) {
        var parent = w.parent;
        var same = false;
        try { same = parent.location.origin === window.location.origin; } catch (e) { same = false; }
        if (!same) break;
        targets.push(parent);
        w = parent;
      }
    } catch (e) { /* stop walking */ }
    if (targets.length === 0) targets.push(window.parent || window);
    for (var i = 0; i < targets.length; i++) {
      try { targets[i].addEventListener("message", onHostMessage, false); } catch (e) { /* ignore */ }
    }
  })();

  /* Runs inside the editor via callCommand: no access to this closure. */
  function applyOpsCommand() {
    var ops = (Asc.scope && Asc.scope.weragOps) || [];
    var doc = Api.GetDocument();
    var applied = 0;
    var failed = [];
    var lastInsert = null; // {key, para}: consecutive inserts at one anchor keep their order

    function norm(s) {
      s = String(s == null ? "" : s);
      if (s.normalize) s = s.normalize("NFC");
      return s.replace(/[\u200B-\u200D\uFEFF]/g, "").replace(/[\s\u00A0]+/g, " ").trim();
    }
    function findPara(anchor) {
      var want = norm(anchor && anchor.text);
      if (!want) return null;
      // n-th match only: with fewer matches than `occurrence` the op fails
      // (never falls back to the first match).
      var n = Math.max(1, Math.floor((anchor && anchor.occurrence) || 1));
      var ps = doc.GetAllParagraphs();
      for (var i = 0; i < ps.length; i++) {
        if (norm(ps[i].GetText()) === want) {
          n -= 1;
          if (n === 0) return ps[i];
        }
      }
      return null;
    }
    function mustFind(anchor) {
      var p = findPara(anchor);
      if (!p) throw new Error("anchor not found");
      return p;
    }
    function firstRun(p) {
      var n = p.GetElementsCount();
      for (var i = 0; i < n; i++) {
        var e = p.GetElement(i);
        if (e && e.GetClassType && e.GetClassType() === "run" && e.GetText() !== "") return e;
      }
      return null;
    }
    // Replace the paragraph's content, keeping paragraph props and the first
    // run's text props (ApiParagraph.SetText would drop the run formatting).
    function rebuild(p, text) {
      var fr = firstRun(p);
      var tp = fr ? fr.GetTextPr() : null;
      p.RemoveAllElements();
      var run = p.AddText(text);
      if (tp && run) run.SetTextPr(tp);
      return run;
    }
    function red(target) {
      if (typeof Api.HexColor === "function") target.SetColor(Api.HexColor("#FF0000"));
      else target.SetColor(255, 0, 0, false); // DS < 9: SetColor(r, g, b, isAuto)
    }
    function countBefore(text, sub, end) {
      var c = 0;
      var i = text.indexOf(sub);
      while (i !== -1 && i < end) {
        c += 1;
        i = text.indexOf(sub, i + sub.length);
      }
      return c;
    }
    function anchorKey(a) {
      return a && a.atStart ? "\u0000start" : norm(a && a.text) + "\u0000" + ((a && a.occurrence) || 1);
    }

    var handlers = {
      replaceText: function (op) {
        var p = mustFind(op.anchor);
        var before = p.GetText();
        var i0 = before.indexOf(op.old);
        var hits = i0 === -1 ? [] : p.Search(op.old, true) || [];
        if (hits.length > 0) {
          // Range-level edit keeps the surrounding run formatting: insert the
          // new text before the match, then delete the (re-located) old text.
          if (op["new"]) {
            hits[0].AddText(op["new"], "before");
            var k = countBefore(p.GetText(), op.old, i0 + op["new"].length);
            var again = p.Search(op.old, true) || [];
            var target = again[k];
            if (!target || target.GetText() !== op.old) throw new Error("could not re-locate old text");
            target.Delete();
          } else {
            hits[0].Delete();
          }
          return;
        }
        // Fallback: whitespace-normalised match → rebuild the paragraph.
        var flat = norm(before);
        var oldFlat = norm(op.old);
        var j = oldFlat ? flat.indexOf(oldFlat) : -1;
        if (j === -1) throw new Error("text not found");
        rebuild(p, flat.slice(0, j) + op["new"] + flat.slice(j + oldFlat.length));
      },
      replaceParagraph: function (op) {
        rebuild(mustFind(op.anchor), op["new"]);
      },
      insertAfter: function (op) {
        var key = anchorKey(op.anchor);
        var ref;
        var pos = "after";
        if (lastInsert && lastInsert.key === key) {
          ref = lastInsert.para;
        } else if (op.anchor && op.anchor.atStart) {
          var ps = doc.GetAllParagraphs();
          if (!ps.length) throw new Error("empty document");
          ref = ps[0];
          pos = "before";
        } else {
          ref = mustFind(op.anchor);
        }
        var model = (op.like && findPara(op.like)) || ref;
        var np = model.Copy(); // keeps paragraph props (alignment, indents, spacing, style)
        np.RemoveAllElements();
        var fr = firstRun(model);
        var text = typeof op.text === "string" ? op.text : ""; // blank line: server omits it
        var run = text ? np.AddText(text) : null;
        if (fr && run) run.SetTextPr(fr.GetTextPr());
        if (op.alignment) np.SetJc(op.alignment);
        if (run && typeof op.bold === "boolean") run.SetBold(op.bold);
        if (run && typeof op.italic === "boolean") run.SetItalic(op.italic);
        var inserted = ref.InsertParagraph(np, pos, true);
        lastInsert = { key: key, para: inserted || np };
      },
      mark: function (op) {
        var p = mustFind(op.anchor);
        var target = p;
        if (op.text) {
          var hits = p.Search(op.text, true) || [];
          if (!hits.length) throw new Error("text not found");
          target = hits[0];
        }
        if (op.style === "highlight") {
          target.SetHighlight("yellow");
        } else if (op.style === "color") {
          red(target);
        } else {
          // "underline": the Office API only has SetUnderline(bool) — no wavy
          // style / underline colour — so it is a plain underline in red text.
          target.SetUnderline(true);
          red(target);
        }
      },
      formatParagraph: function (op) {
        var p = mustFind(op.anchor);
        if (op.alignment) p.SetJc(op.alignment);
        if (op.font) p.SetFontFamily(op.font);
        if (typeof op.sizePt === "number" && op.sizePt > 0) p.SetFontSize(Math.round(op.sizePt * 2)); // half-points
        if (typeof op.bold === "boolean") p.SetBold(op.bold);
        if (typeof op.italic === "boolean") p.SetItalic(op.italic);
      },
      pageSetup: function (op) {
        var secs = doc.GetSections() || [];
        if (!secs.length) throw new Error("no section");
        var m = op.marginsMm;
        var tw = function (mm) { return Math.round((mm * 1440) / 25.4); };
        for (var i = 0; i < secs.length; i++) {
          var s = secs[i];
          if (op.a4) s.SetPageSize(11906, 16838, true); // twips, portrait
          if (m) {
            s.SetPageMargins(
              typeof m.left === "number" ? tw(m.left) : s.GetPageMarginLeft(),
              typeof m.top === "number" ? tw(m.top) : s.GetPageMarginTop(),
              typeof m.right === "number" ? tw(m.right) : s.GetPageMarginRight(),
              typeof m.bottom === "number" ? tw(m.bottom) : s.GetPageMarginBottom()
            );
          }
        }
      },
    };

    for (var i = 0; i < ops.length; i++) {
      var op = ops[i];
      try {
        var h = op && handlers[op.op];
        if (!h) throw new Error("unknown op");
        if (op.op !== "insertAfter") lastInsert = null;
        h(op);
        applied += 1;
      } catch (e) {
        failed.push({ index: i, error: String((e && e.message) || e) });
      }
    }
    return JSON.stringify({ applied: applied, failed: failed });
  }
})(window);
