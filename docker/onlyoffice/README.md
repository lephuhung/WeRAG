# ONLYOFFICE Document Server (document assistant)

The `documentserver` service in `docker-compose.yml` (profile `onlyoffice`)
hosts the editor embedded in the chat UI. The backend talks to it through
three URLs:

| Variable | Direction | Example |
|---|---|---|
| `ONLYOFFICE_PUBLIC_URL` | browser -> DS (loads `web-apps/apps/api/documents/api.js`) | `http://localhost:8090` |
| `ONLYOFFICE_INTERNAL_URL` | backend -> DS (`/command`, `/converter`) | `http://documentserver` |
| `ONLYOFFICE_BACKEND_URL` | DS -> backend (`/r/<grant>` downloads, `/onlyoffice/callback/<ticket>` saves) | `http://app:8080` |

`ONLYOFFICE_JWT_SECRET` must equal the DS `JWT_SECRET` (compose passes the
same variable to both). The feature is off unless `ONLYOFFICE_PUBLIC_URL` and
`ONLYOFFICE_JWT_SECRET` are set.

```bash
docker compose --profile onlyoffice up -d documentserver
```

## Fonts (Times New Roman for NĐ30 documents)

Administrative documents (Nghị định 30/2020/NĐ-CP) use Times New Roman. DS
does not ship it, so layout and line breaks drift unless a metric-compatible
font is installed. Put `.ttf` files in `docker/onlyoffice/fonts/` (mounted at
`/usr/share/fonts/truetype/custom`):

- **Times New Roman** — copy `times.ttf`, `timesbd.ttf`, `timesi.ttf`,
  `timesbi.ttf` from a licensed Windows install (`C:\Windows\Fonts`). Do not
  commit them: the font is proprietary.
- **Tinos** (free, Apache-2.0, metric-compatible with Times New Roman) —
  download from Google Fonts / the ChromeOS core fonts and copy
  `Tinos-*.ttf` here.

Then rebuild the DS font cache (needed after every font change):

```bash
docker exec WeKnora-documentserver /usr/bin/documentserver-generate-allfonts.sh
```

Reload the editor in the browser afterwards (the font list is cached per
session).

## Plugin

`docker/onlyoffice/plugins/werag-assistant` is mounted into
`/var/www/onlyoffice/documentserver/sdkjs-plugins/werag-assistant` and
autostarted by the editor config (`asc.{7a4b3c2d-9e1f-4a5b-8c6d-0e1f2a3b4c5d}`).
It reports the user's selection to the chat page.
