# Thiết kế: Theme giao diện toàn cục — preset Anthropic

Ngày: 2026-10-11
Trạng thái: chờ duyệt
Nguồn thiết kế: `/home/windowsId/portal/anthropic-DESIGN.md`

## 1. Mục tiêu và phạm vi

Thêm khả năng đổi **họ giao diện (theme)** toàn cục cho WeRAG, do **SuperAdmin
(SystemAdmin)** chọn và **áp dụng cho mọi người dùng**. Bản này thêm **một preset
mới "Anthropic"** bên cạnh preset mặc định hiện tại ("ElevenLabs"); không xây
registry theme mở rộng.

Phạm vi áp dụng: **toàn bộ WeRAG web** (`frontend-next`) **và toàn bộ Word
add-in** (`/home/werag-word-addin`). Theme gắn ở tầng `<html>` nên mọi trang /
panel đổi theo; add-in đổi toàn bộ taskpane.

Ngoài phạm vi: portal `it-asset-inventory` (`/home/windowsId/portal`), lựa chọn
theme theo từng người dùng, thêm preset thứ ba, chuyển theme Notion của portal.

## 2. Quyết định đã chốt

1. **Mô hình:** SuperAdmin chọn họ giao diện toàn cục ∈ {`elevenlabs` (mặc
   định), `anthropic`}. Mỗi người dùng vẫn tự chọn **Sáng/Tối/Theo hệ thống**
   như hiện tại (`weknora_theme` trong `localStorage`, `html.dark`).
2. **Lưu trữ:** dùng **registry `system_settings`** sẵn có của WeRAG (3 tầng
   DB > ENV > default), không thêm bảng/migration.
3. **Phân phối:** thêm endpoint **công khai** `GET /api/v1/system/ui-theme` để
   cả trang login (chưa đăng nhập) cũng nhận đúng theme.
4. **Font:** theme Anthropic dùng **IBM Plex Sans** (display + body) và **IBM
   Plex Mono** (code) thay cho "Anthropic Sans/Mono" độc quyền. Cả hai có subset
   tiếng Việt. Theme ElevenLabs giữ nguyên Inter + Newsreader.
5. **Dark variant:** `anthropic-DESIGN.md` chỉ có palette sáng; bản này **suy ra
   dark variant** từ chính token Anthropic (xem §5) để light/dark vẫn hoạt động.
6. **Anti-FOUC:** script inline trong `<head>` đọc cache `localStorage` và gắn
   `data-theme` trước khi paint; một client component fetch giá trị authoritative
   rồi đồng bộ. Add-in `await` fetch trước khi render.

## 3. Kiến trúc tổng quan

```
SuperAdmin (Settings → General)
        │ PUT /api/v1/system/admin/settings/ui.theme   (SystemAdmin, có audit)
        ▼
system_settings row  ui.theme = "anthropic"
        │ GET /api/v1/system/ui-theme                  (công khai, mọi user)
        ▼
frontend-next  ──► <html data-theme="anthropic">  ──► styles/theme-anthropic.css
word add-in    ──► <html data-theme="anthropic">  ──► src/theme-anthropic.css
```

`data-theme` độc lập với `html.dark`. Tổ hợp bốn trạng thái có thể:
`(không có data-theme)` = ElevenLabs sáng; `.dark` = ElevenLabs tối;
`data-theme="anthropic"` = Anthropic sáng; `data-theme="anthropic".dark` =
Anthropic tối.

## 4. Backend

### 4.1 Registry key

Trong `internal/application/service/system_setting.go`:

- Thêm hằng `const SystemUIThemeKey = "ui.theme"`.
- Thêm entry vào `registry`:

  | Thuộc tính | Giá trị |
  |---|---|
  | `Type` | `"string"` |
  | `EnvName` | `"WEKNORA_UI_THEME"` (tuỳ chọn — cho phép default theo môi trường) |
  | `Default` | `"elevenlabs"` |
  | `Enum` | `["elevenlabs", "anthropic"]` |
  | `Category` | `"appearance"` |
  | `Description` | Mô tả ngắn: họ giao diện toàn cục; đổi áp dụng ngay cho mọi user. |

`Update` sẵn có validate kiểu + enum và ghi audit `system.setting_changed`, nên
không cần logic mới. Không cần `RequiresRestart`.

### 4.2 Endpoint đọc công khai

- Handler mới `SystemHandler.GetSystemUITheme` trong `internal/handler/system.go`,
  mô phỏng `GetSystemParseDefaults`: đọc qua `systemSettingSvc.GetString(ctx,
  service.SystemUIThemeKey, "WEKNORA_UI_THEME", "elevenlabs")`, trả
  `200 {"success": true, "data": {"theme": "<value>"}}`.
- Route trong `RegisterSystemRoutes` (`internal/router/routes_auth_tenant.go`):
  tách group thô `/system` khỏi `apiKeyGroup`, rồi đăng ký **trên group thô,
  không có `g.Member()`** để thành route công khai:
  ```go
  systemGroup := r.Group("/system")
  systemRoutes := g.apiKeyGroup(systemGroup, apiKeyManageVectorStores(apiKeyFullAccess()))
  ...
  systemGroup.GET("/ui-theme", handler.GetSystemUITheme) // công khai, API key bị default-deny
  ```
  Vì khai báo ngoài `apiKeyGroup`, `assertAPIKeyPoliciesMatchRoutes` không đòi
  policy cho route này và API key mặc định bị từ chối. Route không nhạy cảm
  (chỉ là tên theme).
- **Bắt buộc:** `middleware.Auth` chạy **toàn cục** (`r.Use` trong
  `internal/router/router.go`) và chặn mọi request không nằm trong allowlist
  `noAuthAPI` (`internal/middleware/auth.go`) bằng 401. Vì vậy phải thêm
  `"/api/v1/system/ui-theme": {"GET"}` vào allowlist đó; nếu không route vẫn
  401 dù đã bỏ `g.Member()`. Đây là điểm bản thiết kế đầu bỏ sót, phát hiện khi
  chạy thử thực tế.

### 4.3 Kiểm thử backend

- `internal/application/service/system_setting_validation_test.go`: thêm
  `TestValidateUITheme` — nhận `"elevenlabs"`, `"anthropic"`; từ chối giá trị
  ngoài enum và giá trị không phải string.
- `internal/handler/`: test mới cho `GetSystemUITheme` theo đúng mẫu fake
  `SystemSettingService` đang dùng ở các test `system_*_test.go` — row có giá
  trị → trả đúng; row thiếu/DB lỗi → trả default `"elevenlabs"`, không 500.
- `internal/router/routes_platform_config_test.go`: không cần đổi (route công
  khai không nằm trong danh sách phải 403). Chạy lại để xác nhận.

## 5. Token CSS Anthropic (light + dark suy ra)

File dùng chung `styles/theme-anthropic.css` (nguồn tại `frontend-next`, được
copy sang add-in — xem §10). Nội dung: override token dưới selector
`html[data-theme="anthropic"]` (light) và `html[data-theme="anthropic"].dark`
(dark). Selector unlayered nên thắng `@theme` (trong `@layer theme`).

| Token | ElevenLabs | Anthropic Light | Anthropic Dark (suy ra) |
|---|---|---|---|
| `--color-canvas` | `#f5f5f5` | `#faf9f5` | `#141413` |
| `--color-canvas-soft` | `#fafafa` | `#f5f4ee` | `#1a1917` |
| `--color-canvas-deep` | `#0c0a09` | `#141413` | `#0c0b0a` |
| `--color-surface-card` | `#ffffff` | `#ffffff` | `#1e1d1b` |
| `--color-surface-strong` | `#f0efed` | `#f1efe8` | `#26251f` |
| `--color-surface-dark` | `#0c0a09` | `#141413` | `#0f0e0d` |
| `--color-surface-dark-elevated` | `#1c1917` | `#26251f` | `#1e1d1b` |
| `--color-primary` | `#292524` | `#141413` | `#faf9f5` |
| `--color-primary-active` | `#0c0a09` | `#000000` | `#ffffff` |
| `--color-ink` | `#0c0a09` | `#141413` | `#faf9f5` |
| `--color-body` | `#4e4e4e` | `#4a4843` | `#c4c0b8` |
| `--color-body-strong` | `#292524` | `#141413` | `#faf9f5` |
| `--color-muted` | `#777169` | `#6f6b64` | `#a5a099` |
| `--color-muted-soft` | `#a8a29e` | `#a5a099` | `#6f6b64` |
| `--color-on-primary` | `#ffffff` | `#ffffff` | `#141413` |
| `--color-on-dark` | `#ffffff` | `#ffffff` | `#faf9f5` |
| `--color-on-dark-soft` | `#a8a29e` | `#cfcbc3` | `#a5a099` |
| `--color-hairline` | `#e7e5e4` | `#e6e3da` | `#2e2d29` |
| `--color-hairline-soft` | `#f0efed` | `#efede6` | `#232220` |
| `--color-hairline-strong` | `#d6d3d1` | `#d5d1c6` | `#3d3b36` |
| `--radius-sm2` | `6px` | `8px` | `8px` |
| `--radius-md2` | `8px` | `8px` | `8px` |
| `--radius-lg2` | `12px` | `16px` | `16px` |
| `--radius-xl2` | `16px` | `16px` | `16px` |
| `--radius-xxl` | `24px` | `24px` | `24px` |
| `--shadow-soft-drop` | `0 4px 16px rgba(0,0,0,.04)` | `rgba(0,0,0,.01) 0 2px 2px, rgba(0,0,0,.02) 0 4px 4px, rgba(0,0,0,.04) 0 16px 24px` | giữ như light |

- Giữ nguyên `--color-success` (`#16a34a`), `--color-error` (`#dc2626`) và
  palette sticker (`sky`, `emerald`, `amber`, `rose`, `violet`, `blue`, `orb-*`)
  để không phá ngữ nghĩa trạng thái. Anthropic spec là palette achromatic; các
  màu trạng thái là ngoại lệ có chủ đích.
- Light: kiểm tra tương phản chữ trên nền — `#141413` trên `#faf9f5` ≈ 20:1
  (AAA), `--color-body #4a4843` trên `#faf9f5` ≈ 8.6:1 (AAA). Dark:
  `#faf9f5` trên `#141413` ≈ 20:1; `--color-body #c4c0b8` trên `#141413` ≈ 9:1.
  `color-scheme: dark` cho block dark.

## 6. Font

### 6.1 Biến font trung gian

`styles/theme-anthropic.css` chỉ remap token theo **biến do từng app cấp**, để
một file dùng chung chạy được ở cả hai nơi:

```css
html[data-theme="anthropic"] {
  --font-sans:    var(--ui-font-sans, ui-sans-serif), system-ui, "Segoe UI", Arial, sans-serif;
  --font-display: var(--ui-font-sans, ui-sans-serif), system-ui, "Segoe UI", Arial, sans-serif;
  --font-mono:    var(--ui-font-mono, ui-monospace), Consolas, "Courier New", monospace;
}
```

Hệ quả có chủ đích: theme Anthropic **bỏ serif ở display** (`.display-*` chuyển
sang sans đậm) — điểm khác biệt rõ nhất so với ElevenLabs.

### 6.2 Web (`frontend-next`)

Nạp trong `app/layout.tsx` bằng `next/font/google`, tự self-host, có subset
tiếng Việt:

```ts
import { IBM_Plex_Sans, IBM_Plex_Mono } from "next/font/google";
const plexSans = IBM_Plex_Sans({ subsets: ["latin", "vietnamese"],
  weight: ["400", "500", "600", "700"], variable: "--ui-font-sans", display: "swap" });
const plexMono = IBM_Plex_Mono({ subsets: ["latin", "vietnamese"],
  weight: ["400", "500"], variable: "--ui-font-mono", display: "swap" });
```

Gắn `plexSans.variable` và `plexMono.variable` vào `className` của `<html>`.

### 6.3 Add-in

Theo đúng cách add-in tự host Inter hiện nay:

- Thêm woff2 IBM Plex (Sans 400/500/600/700, Mono 400/500) vào
  `/home/werag-word-addin/public/fonts/`.
- `src/anthropic-fonts.css`: `@font-face` cho các file trên, đường dẫn tính theo
  Vite `base: "/word/"`, kèm khai báo
  `:root { --ui-font-sans: "IBM Plex Sans", "Segoe UI", Arial, sans-serif;
  --ui-font-mono: "IBM Plex Mono", Consolas, monospace; }`.
- `src/styles.css` `@import "./anthropic-fonts.css";` và
  `@import "./styles/theme-anthropic.css";`.

Fallback cuối stack là `"Segoe UI"` / `Consolas` nên nếu thiếu file woff2, giao
diện vẫn khác Inter (vẫn là đổi font) và không vỡ layout.

### 6.4 Cỡ chữ

Giữ **cỡ chữ body 14px** như hiện tại (không ép 12px theo spec) để tránh vỡ
layout; chỉ token màu/bo góc/bóng và font đổi.

## 7. Bootstrap chống nháy (FOUC)

### 7.1 Module dùng chung `lib/ui-theme.ts`

- `UI_THEME_ATTR = "data-theme"`, `UI_THEME_STORAGE_KEY = "weknora_ui_theme"`,
  hai giá trị hợp lệ `"elevenlabs" | "anthropic"`.
- `applyUiTheme(theme)`: chỉ set `documentElement.dataset.theme` khi khác default
  (`elevenlabs` → gỡ attribute), ghi `localStorage` (bọc try/catch).
- `readCachedUiTheme()`: đọc localStorage an toàn.
- `fetchUiTheme()`: gọi `GET /api/v1/system/ui-theme` qua `apiGet` sẵn có, trả
  giá trị đã chuẩn hoá; lỗi mạng/parse → trả `"elevenlabs"` (không throw).
- `subscribeUiTheme(fn)`: nghe sự kiện `window` `"weknora:ui-theme-changed"` để
  UI admin cập nhật tức thì mà không cần reload.

### 7.2 Web

- `app/layout.tsx`: thêm `<script>` inline trong `<head>` chạy trước paint:
  đọc `localStorage["weknora_ui_theme"]`, nếu `"anthropic"` thì set
  `documentElement.dataset.theme`. Mount `<GlobalUiTheme />` (client) một lần
  trong `<body>` để `fetchUiTheme()` rồi `applyUiTheme`.
- Lần truy cập đầu tiên (cache trống) có thể nháy đúng một nhịp; các lần sau
  sạch. Chấp nhận được vì theme đổi rất ít.

### 7.3 Add-in

`src/main.tsx`: gọi `readCachedUiTheme()` + `applyUiTheme()` ngay (đồng bộ),
rồi `await fetchUiTheme()` với timeout ~1.5s **trước** `createRoot(...).render`
để không nháy. Nếu fetch lỗi/timeout, dùng cache/default.

## 8. UI SuperAdmin

Trong `frontend-next/components/settings/general-settings.tsx`, thêm một dòng
**"Theme hệ thống"** chỉ render khi `useAuth().user?.is_system_admin === true`:

- Giá trị hiện tại: `getSystemSetting(SystemUIThemeKey)` trong `lib/api/system.ts`
  (đọc `value` string; thiếu row → `"elevenlabs"`).
- Đổi: `updateSystemSetting(SystemUIThemeKey, v)` → `applyUiTheme(v)` +
  dispatch `"weknora:ui-theme-changed"` + hiện toast lỗi/thành công.
- Thêm khoá i18n (vi + en) vào `lib/i18n.tsx`: tiêu đề, mô tả, nhãn hai lựa chọn.
- Người dùng thường không thấy dòng này; preset vẫn áp cho họ qua fetch.

## 9. Add-in

- Không có UI chọn theme; add-in chỉ đọc cấu hình toàn cục.
- Thay đổi: `src/styles.css` import thêm font + token Anthropic; `src/main.tsx`
  bootstrap; `src/lib/ui-theme.ts` (copy dùng chung); `public/fonts/` thêm woff2.

## 10. Đồng bộ code dùng chung

`scripts/shared-files.json` của add-in thêm hai mục:

- `styles/theme-anthropic.css`
- `lib/ui-theme.ts`

Sau khi sửa upstream `frontend-next`, chạy
`npm run sync-shared -- /home/WeRAG/frontend-next` trong repo add-in, rồi rà
`git diff` (đúng quy ước CLAUDE.md của add-in). Lưu ý `src/styles.css` **không**
nằm trong danh sách sync — phần `@import` thêm tay một lần; riêng file token và
module font là đồng bộ được.

## 11. Kiểm thử và tiêu chí nghiệm thu

### 11.1 Backend

- `go test ./internal/application/service/... ./internal/handler/... ./internal/router/...`
  xanh, gồm `TestValidateUITheme` và test `GetSystemUITheme`.
- Thủ công: `PUT /api/v1/system/admin/settings/ui.theme {"value":"anthropic"}`
  → 200 + audit; giá trị ngoài enum → 400; `DELETE .../ui.theme` → về default.

### 11.2 Web

- `cd frontend-next && npm run typecheck` xanh, và
  `node --experimental-strip-types --test lib/ui-theme.test.ts` xanh (web không có
  vitest; test chạy bằng node built-in như các test `lib/*.test.ts` khác).
- Unit test cho `lib/ui-theme.ts`: chuẩn hoá giá trị, `applyUiTheme` gỡ attribute
  khi default, `fetchUiTheme` lỗi → default.
- Thủ công: theme=anthropic → `<html data-theme="anthropic">`, nền ấm `#faf9f5`,
  heading sans (hết serif), font mono đổi; bật Tối → dark variant; đổi lại
  elevenlabs → về Inter/Newsreader. Kiểm tra trang login cũng đổi.

### 11.3 Add-in

- `cd /home/werag-word-addin && npm run typecheck && npm test && npm run test:e2e`
  xanh; thêm unit test `ui-theme` theo mẫu `node --test` sẵn có.
- Thủ công (hoặc e2e giả lập): `data-theme` được gắn trước khi app hiện; theme
  add-in khớp theme web cùng backend.

### 11.4 Tiêu chí nghiệm thu

1. SuperAdmin đổi theme; một user khác reload web nhận theme mới.
2. Add-in cùng backend nhận đúng theme, không nháy.
3. Light/dark vẫn hoạt động ở cả hai theme.
4. Theme ElevenLabs không đổi hành vi/hình dạng.
5. Không hard-code hex trong component; chỉ thêm trong file token.

## 12. Rủi ro

- **Thiếu woff2 IBM Plex cho add-in** (cần mạng khi triển khai): đã có fallback
  `"Segoe UI"`/`Consolas`; web (next/font) không bị ảnh hưởng.
- **Vỡ layout do đổi font/bo góc**: giữ cỡ chữ; kiểm thử hình ảnh add-in
  (`test-results/`) và rà vài trang web đại diện.
- **Hard-code màu ngoài token**: rà `frontend-next` + add-in xem còn hex trực
  tiếp trong vùng bị theme không; chỉ xử lý trong phạm vi cần thiết.
- **Route công khai mới**: chỉ trả tên theme, không nhạy cảm; vẫn bị API key
  default-deny.

## 13. Ngoài phạm vi

- Portal `it-asset-inventory` và theme Notion của nó.
- Lựa chọn theme per-user hoặc per-tenant.
- Preset thứ ba trở lên / registry theme mở rộng trong UI.
- Đổi cỡ chữ body theo spec Anthropic (12px).
- Cập nhật `anthropic-DESIGN.md` hoặc tạo bản sao trong repo.
