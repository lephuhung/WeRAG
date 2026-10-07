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

Office API used: `Api.GetDocument`, `ApiDocument.GetAllParagraphs/GetSections`,
`ApiParagraph.GetText/Search/Copy/RemoveAllElements/AddText/InsertParagraph/
GetElement/GetElementsCount/SetJc/SetFontFamily/SetFontSize/SetBold/SetItalic/
SetUnderline/SetHighlight/SetColor`, `ApiRange.AddText/Delete/GetText/
SetUnderline/SetHighlight/SetColor`, `ApiRun.GetTextPr/SetTextPr/SetBold/
SetItalic`, `ApiSection.SetPageSize/SetPageMargins/GetPageMargin*`,
`Api.HexColor` (falls back to `SetColor(r, g, b, false)` on older DS).

Limitations: `SetUnderline` only takes a boolean (no wavy style or underline
colour), so `mark` with `style: "underline"` is a plain underline in red text.
Anchors are matched by whitespace-normalised NFC paragraph text
(`normalizeAnchorText` in `frontend-next/lib/api/document-ops.ts`).
