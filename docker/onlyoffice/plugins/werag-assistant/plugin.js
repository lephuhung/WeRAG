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

  var DEBOUNCE_MS = 300;
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

  window.Asc.plugin.init = function (text) {
    var value = typeof text === "string" ? text.trim() : "";
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
  };

  // Background plugin: no buttons/window, but the API expects a handler.
  window.Asc.plugin.button = function () {};
})(window);
