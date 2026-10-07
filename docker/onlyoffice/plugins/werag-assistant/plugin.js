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
})(window);
