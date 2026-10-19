# Word add-in build for `/word/`

The `frontend` container serves the Word add-in at `/word/` from
`WORD_ADDIN_DIST_DIR` (default: `docker/word-addin/dist`, git-ignored).
Point it at the add-in repository's build output, or copy the build here:

```sh
cd /path/to/werag-word-addin && npm run build
# .env: WORD_ADDIN_DIST_DIR=/path/to/werag-word-addin/dist
npm run manifest -- https://<site origin>   # manifest/manifest.xml for the GPO catalog
```
