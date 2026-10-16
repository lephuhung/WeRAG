# WeRAG Assistant — ONLYOFFICE plugin

Background (non-visual) plugin for ONLYOFFICE Docs that forwards the text the
user selects in the document to the WeRAG chat ("document assistant" agent).
The WeRAG page shows it as a "Đoạn đã chọn" chip above the composer.

Message posted to the embedding page (`window.top`):

```js
{ source: "werag-onlyoffice", type: "selection", text: "…" }
```

WeRAG accepts it only when `event.origin` equals the Document Server origin
(`new URL(document_server_url).origin`).

## Install

Mount this directory into the Document Server container:

```yaml
services:
  onlyoffice:
    volumes:
      - ./docker/onlyoffice/plugins/werag-assistant:/var/www/onlyoffice/documentserver/sdkjs-plugins/werag-assistant:ro
```

`index.html` loads `../v1/plugins.js`, which ships with the Document Server
under `sdkjs-plugins/v1/`, so no internet access is needed.

Restart the container. Run `documentserver-pluginsmanager.sh --update` (or
recreate the container) if the plugin does not appear.

## Caching

The Document Server's own nginx (`includes/ds-docservice.conf`) serves every
file under `sdkjs-plugins/` with `Cache-Control: public, max-age=31536000,
immutable`, so a browser that opened the editor once keeps `config.json` and
`index.html` for a year without asking again. A `?v=` bump inside
`index.html` therefore never reaches it (on 2026-10-15 the new `select` op
failed with `unknown op` for that reason).

- `index.html` loads `plugin.js?t=<Date.now()>`, so a change to `plugin.js`
  takes effect on the next editor start: edit `plugin.js` freely.
- Do **not** change `index.html` or `config.json` unless users are told to
  clear the browser cache for the Document Server origin (or DevTools →
  Network → "Disable cache", reload).
- A browser still on an older `index.html` (before the `?t=` loader) needs
  that cache clear once.
- The web shows "Editor đang chạy bản plugin WeRAG cũ…" when the plugin
  answers `unknown op` / `select unsupported` to a jump in the spelling list.

## Enable it for every editor session

A background plugin must be started. The backend that signs the
`DocsAPI.DocEditor` config should include:

```json
"editorConfig": {
  "plugins": {
    "autostart": ["asc.{7a4b3c2d-9e1f-4a5b-8c6d-0e1f2a3b4c5d}"],
    "options": {
      "asc.{7a4b3c2d-9e1f-4a5b-8c6d-0e1f2a3b4c5d}": { "hostOrigin": "https://werag.example.vn" }
    }
  }
}
```

`hostOrigin` is the WeRAG frontend origin; the plugin uses it as the
`postMessage` target origin. Without it, the plugin tries
`location.ancestorOrigins` and only then falls back to `"*"`. The payload is
the user's own selection, sent to the page that already shows it.

Because `editorConfig` is covered by the JWT, these keys must be added
server-side before signing.

## AI edit plans (`apply_ops`)

The editing tools of the document assistant return an edit plan instead of
writing the file. WeRAG posts it to the editor iframe:

```js
{ source: "werag-host", type: "apply_ops", batchId: "…", ops: [/* Op[] */] }
```

The plugin listens on its same-origin ancestor frames (the editor frame),
accepts the message only from `hostOrigin`, answers
`{source:"werag-onlyoffice", type:"ops_ack", batchId}` at once, applies the
whole batch in one `Asc.plugin.callCommand(func, false, true, cb)` (data via
`Asc.scope`) and answers
`{source:"werag-onlyoffice", type:"ops_result", batchId, applied, failed:[{index, error}]}`.
A batch id is applied at most once per editor session (re-posts get the
stored result).

Live spellcheck: `{source:"werag-host", type:"read_paragraphs", requestId}`
is answered with `{source:"werag-onlyoffice", type:"paragraphs", requestId,
texts:[...]}` (every paragraph's text, read-only). The host reviews the new
or changed paragraphs and sends an `apply_ops` batch of `unmark` (clears the
runs of a paragraph that are underlined and #FF0000, i.e. earlier red marks;
needs `ApiTextPr.GetUnderline/GetColor`, DS 8.1+) and `mark` ops.

Office API used: `Api.GetDocument`, `ApiDocument.GetAllParagraphs/GetSections`,
`ApiParagraph.GetText/Search/Copy/RemoveAllElements/AddText/InsertParagraph/
GetElement/GetElementsCount/SetJc/SetFontFamily/SetFontSize/SetBold/SetItalic/
SetUnderline/SetHighlight/SetColor`, `ApiRange.AddText/Delete/GetText/
SetUnderline/SetHighlight/SetColor`, `ApiRun.GetTextPr/SetTextPr/SetBold/
SetItalic`, `ApiSection.SetPageSize/SetPageMargins/GetPageMargin*`,
`Api.HexColor` (falls back to `SetColor(r, g, b, false)` on older DS),
`Api.CreateParagraph/CreateImage`, `ApiParagraph.AddDrawing` (`insertImage`).

`select` (`{anchor, text?, textOccurrence?}`) is never sent by the server
either: clicking a mistake in the spelling ring's list moves the selection
to it (`ApiRange.Select`), so the editor scrolls there; nothing changes.

`insertImage` (`{anchor, src, widthMm, heightMm?, caption?}`) is never sent by
the server: the chat card of `draw_diagram` / `generate_illustration` builds
it when the user clicks "Chèn vào văn bản", with `src` a PNG data URL.

Limitations: `SetUnderline` only takes a boolean (no wavy style or underline
colour), so `mark` with `style: "underline"` is a plain underline in red text.
Anchors are matched by whitespace-normalised NFC paragraph text
(`normalizeAnchorText` in `frontend-next/lib/api/document-ops.ts`).
