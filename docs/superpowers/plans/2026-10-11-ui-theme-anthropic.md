# UI Theme (Anthropic preset) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Thêm preset giao diện "Anthropic" chọn bởi SuperAdmin, áp dụng toàn cục cho WeRAG web và Word add-in.

**Architecture:** Lưu họ giao diện trong `system_settings` key `ui.theme`, đọc qua endpoint công khai `GET /api/v1/system/ui-theme`. Hai app gắn `data-theme="anthropic"` lên `<html>`; một file CSS dùng chung override token. Light/dark per-user giữ nguyên.

**Tech Stack:** Go (Gin, GORM) + Next.js 16 / React 19 / Tailwind 4 (`frontend-next`) + Vite / React 19 / Tailwind 4 (`werag-word-addin`).

**Spec:** `docs/superpowers/specs/2026-10-11-ui-theme-anthropic-design.md`

## Global Constraints

- Ngôn ngữ i18n: chỉ `en` và `vi`.
- Không hard-code hex trong component — chỉ trong file token.
- Web không có vitest; test chạy bằng `node --experimental-strip-types --test <file>` (node built-in). Add-in test bằng `node --experimental-strip-types --test "src/**/*.test.ts"`.
- Add-in Vite `base: "/word/"`.
- GitNexus: impact analysis đã chạy — `RegisterSystemRoutes`/`SystemHandler`/`validateRegistryEntry`/`registry` đều LOW. Chạy `detect_changes()` trước commit cuối nếu tool có.
- WeRAG worktree đang có nhiều thay đổi không liên quan — **chỉ `git add` đúng file của task**, không `git add -A`.
- Không chạy `npm run sync-shared` toàn bộ (sẽ kéo thay đổi upstream ngoài phạm vi); chỉ copy tay 2 file mới.

---

### Task 1: Backend — registry key `ui.theme`

**Files:**
- Modify: `internal/application/service/system_setting.go`
- Test: `internal/application/service/system_setting_validation_test.go`

**Interfaces:**
- Produces: `service.SystemUIThemeKey = "ui.theme"` (string const); registry entry type `string`, enum `["elevenlabs","anthropic"]`, default `"elevenlabs"`.

- [ ] **Step 1: Write the failing test**

Append to `internal/application/service/system_setting_validation_test.go`:

```go
func TestValidateUITheme(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		wantErr bool
	}{
		{name: "default", value: "elevenlabs"},
		{name: "anthropic", value: "anthropic"},
		{name: "unknown enum", value: "notion", wantErr: true},
		{name: "wrong type", value: 42, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegistryEntry(SystemUIThemeKey, tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/application/service/ -run TestValidateUITheme -v`
Expected: FAIL — `undefined: SystemUIThemeKey` (hoặc unknown key).

- [ ] **Step 3: Add the const + registry entry**

Trong `internal/application/service/system_setting.go`, cạnh `SystemParseDefaultsKey`:

```go
// SystemUIThemeKey is the system_settings key holding the platform-wide UI
// theme family. Read by the public GET /system/ui-theme endpoint so the web
// app and the Word add-in render the same theme, login page included.
const SystemUIThemeKey = "ui.theme"
```

Thêm entry vào `var registry` (đặt cạnh các entry `auth.*`):

```go
	// ui.theme is the platform-wide UI theme family. "elevenlabs" is the
	// built-in design language; "anthropic" is the Anthropic-derived preset.
	// Read by GET /system/ui-theme; applied at <html data-theme>.
	"ui.theme": {
		Type:     "string",
		EnvName:  "WEKNORA_UI_THEME",
		Default:  "elevenlabs",
		Enum:     []string{"elevenlabs", "anthropic"},
		Category: "appearance",
		Description: "平台级界面主题。elevenlabs = 内置设计语言；anthropic = Anthropic 风格预设。" +
			"修改后立即对所有用户生效，无需重启。",
	},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/application/service/ -run TestValidateUITheme -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/application/service/system_setting.go internal/application/service/system_setting_validation_test.go
git commit -m "feat(system-setting): add ui.theme registry key"
```

---

### Task 2: Backend — public `GET /api/v1/system/ui-theme`

**Files:**
- Modify: `internal/handler/system.go`
- Modify: `internal/router/routes_auth_tenant.go`
- Test: `internal/handler/system_ui_theme_test.go` (new)

**Interfaces:**
- Consumes: `service.SystemUIThemeKey` (Task 1).
- Produces: `SystemHandler.GetSystemUITheme(c *gin.Context)`; route `GET /api/v1/system/ui-theme` → `{"success":true,"data":{"theme":"<elevenlabs|anthropic>"}}`.

- [ ] **Step 1: Write the failing test**

Create `internal/handler/system_ui_theme_test.go`:

```go
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/gin-gonic/gin"
)

type uiThemeTestSettings struct {
	runtimeTestSettings
	theme string
}

func (s uiThemeTestSettings) GetString(_ context.Context, key, _ string, def string) string {
	if key == service.SystemUIThemeKey && s.theme != "" {
		return s.theme
	}
	return def
}

func TestGetSystemUITheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name  string
		theme string
		want  string
	}{
		{name: "anthropic", theme: "anthropic", want: "anthropic"},
		{name: "empty falls back to default", theme: "", want: "elevenlabs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &SystemHandler{systemSettingSvc: uiThemeTestSettings{theme: tt.theme}}
			engine := gin.New()
			engine.GET("/api/v1/system/ui-theme", h.GetSystemUITheme)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/ui-theme", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
			}
			var body struct {
				Success bool `json:"success"`
				Data    struct {
					Theme string `json:"theme"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !body.Success || body.Data.Theme != tt.want {
				t.Fatalf("body = %s, want theme=%q", w.Body.String(), tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/ -run TestGetSystemUITheme -v`
Expected: FAIL — `h.GetSystemUITheme undefined`.

- [ ] **Step 3: Add the handler**

Trong `internal/handler/system.go`, ngay sau `GetSystemParseDefaults`:

```go
// GetSystemUITheme godoc
// @Summary      Get the platform-wide UI theme
// @Description  Returns {"theme": "elevenlabs"|"anthropic"} via the 3-tier
// @Description  system-setting resolver. Public (no auth) so the login page
// @Description  and the Word add-in apply the same theme.
// @Tags         System
// @Produce      json
// @Success      200 {object} map[string]interface{} "{ success, data: { theme } }"
// @Router       /system/ui-theme [get]
func (h *SystemHandler) GetSystemUITheme(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	theme := h.systemSettingSvc.GetString(ctx, service.SystemUIThemeKey, "WEKNORA_UI_THEME", "elevenlabs")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"theme": theme}})
}
```

- [ ] **Step 4: Register the public route**

Trong `internal/router/routes_auth_tenant.go`, `RegisterSystemRoutes`, đổi dòng đầu của group để giữ group thô:

```go
	systemGroup := r.Group("/system")
	systemRoutes := g.apiKeyGroup(systemGroup, apiKeyManageVectorStores(apiKeyFullAccess()))
```

Và thêm trong block `{ ... }`:

```go
		// Public: no g.Member() — the theme name is not sensitive and must be
		// readable before sign-in (login page, add-in first paint). Registered
		// on the raw group so API keys stay default-denied.
		systemGroup.GET("/ui-theme", handler.GetSystemUITheme)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/handler/ -run TestGetSystemUITheme -v && go test ./internal/router/ -run TestPlatformConfigRoutesDenyTenantAdmin -v`
Expected: PASS cả hai.

- [ ] **Step 6: Commit**

```bash
git add internal/handler/system.go internal/router/routes_auth_tenant.go internal/handler/system_ui_theme_test.go
git commit -m "feat(system): public GET /system/ui-theme endpoint"
```

---

### Task 3: Web — token CSS Anthropic + font

**Files:**
- Create: `frontend-next/styles/theme-anthropic.css`
- Modify: `frontend-next/app/globals.css`
- Modify: `frontend-next/app/layout.tsx`

**Interfaces:**
- Produces: selector `html[data-theme="anthropic"]` (+ `.dark`) override token; biến `--ui-font-sans` / `--ui-font-mono` do `app/layout.tsx` cấp.

- [ ] **Step 1: Create `frontend-next/styles/theme-anthropic.css`**

```css
/* Anthropic design preset — source: /home/windowsId/portal/anthropic-DESIGN.md.
 *
 * Selected by SuperAdmin via the system setting `ui.theme` and applied as
 * <html data-theme="anthropic">. Overrides the same CSS variables the default
 * ElevenLabs theme defines in app/globals.css, so every Tailwind utility
 * (bg-canvas, text-ink, border-hairline, …) follows the theme with no
 * component change. Unlayered, so it wins over Tailwind's @layer theme.
 *
 * Shared with the Word add-in (scripts/shared-files.json).
 */

html[data-theme="anthropic"] {
  --color-canvas: #faf9f5;
  --color-canvas-soft: #f5f4ee;
  --color-canvas-deep: #141413;
  --color-surface-card: #ffffff;
  --color-surface-strong: #f1efe8;
  --color-surface-dark: #141413;
  --color-surface-dark-elevated: #26251f;

  --color-primary: #141413;
  --color-primary-active: #000000;
  --color-ink: #141413;
  --color-body: #4a4843;
  --color-body-strong: #141413;
  --color-muted: #6f6b64;
  --color-muted-soft: #a5a099;
  --color-on-primary: #ffffff;
  --color-on-dark: #ffffff;
  --color-on-dark-soft: #cfcbc3;

  --color-hairline: #e6e3da;
  --color-hairline-soft: #efede6;
  --color-hairline-strong: #d5d1c6;

  /* Anthropic radius scale: sm 8 / md 16 / lg 24 */
  --radius-sm2: 8px;
  --radius-md2: 8px;
  --radius-lg2: 16px;
  --radius-xl2: 16px;
  --radius-xxl: 24px;

  --shadow-soft-drop: rgba(0, 0, 0, 0.01) 0 2px 2px 0,
    rgba(0, 0, 0, 0.02) 0 4px 4px 0, rgba(0, 0, 0, 0.04) 0 16px 24px 0;

  /* Anthropic Sans/Mono are proprietary; IBM Plex is the substitute with
   * Vietnamese coverage. Each app provides --ui-font-sans / --ui-font-mono. */
  --font-sans: var(--ui-font-sans, ui-sans-serif), system-ui, "Segoe UI", Arial, sans-serif;
  --font-display: var(--ui-font-sans, ui-sans-serif), system-ui, "Segoe UI", Arial, sans-serif;
  --font-mono: var(--ui-font-mono, ui-monospace), Consolas, "Courier New", monospace;
}

html[data-theme="anthropic"].dark {
  --color-canvas: #141413;
  --color-canvas-soft: #1a1917;
  --color-canvas-deep: #0c0b0a;
  --color-surface-card: #1e1d1b;
  --color-surface-strong: #26251f;
  --color-surface-dark: #0f0e0d;
  --color-surface-dark-elevated: #1e1d1b;

  --color-primary: #faf9f5;
  --color-primary-active: #ffffff;
  --color-ink: #faf9f5;
  --color-body: #c4c0b8;
  --color-body-strong: #faf9f5;
  --color-muted: #a5a099;
  --color-muted-soft: #6f6b64;
  --color-on-primary: #141413;
  --color-on-dark: #faf9f5;
  --color-on-dark-soft: #a5a099;

  --color-hairline: #2e2d29;
  --color-hairline-soft: #232220;
  --color-hairline-strong: #3d3b36;

  color-scheme: dark;
}
```

- [ ] **Step 2: Import it in `frontend-next/app/globals.css`**

Đổi 2 dòng đầu:

```css
@import "tailwindcss";
@import "../styles/theme-anthropic.css";
```

- [ ] **Step 3: Load IBM Plex + boot script in `frontend-next/app/layout.tsx`**

Thêm import và font:

```tsx
import { Inter, Newsreader, IBM_Plex_Sans, IBM_Plex_Mono } from "next/font/google";
```

```tsx
const plexSans = IBM_Plex_Sans({
  subsets: ["latin", "vietnamese"],
  weight: ["400", "500", "600", "700"],
  variable: "--ui-font-sans",
  display: "swap",
});
const plexMono = IBM_Plex_Mono({
  subsets: ["latin", "vietnamese"],
  weight: ["400", "500"],
  variable: "--ui-font-mono",
  display: "swap",
});
```

Đổi `className` của `<html>` để thêm hai biến (giữ Inter/Newsreader):

```tsx
<html
  lang="en"
  className={`${inter.variable} ${displaySerif.variable} ${plexSans.variable} ${plexMono.variable}`}
>
```

- [ ] **Step 4: Verify typecheck**

Run: `cd frontend-next && npm run typecheck`
Expected: PASS (chưa có `data-theme` động, chỉ CSS + font).

- [ ] **Step 5: Commit**

```bash
git add frontend-next/styles/theme-anthropic.css frontend-next/app/globals.css frontend-next/app/layout.tsx
git commit -m "feat(frontend): Anthropic theme tokens + IBM Plex fonts"
```

---

### Task 4: Web — module `ui-theme` + bootstrap chống nháy

**Files:**
- Create: `frontend-next/lib/ui-theme.ts`
- Create: `frontend-next/lib/ui-theme.test.ts`
- Create: `frontend-next/components/global-ui-theme.tsx`
- Modify: `frontend-next/app/layout.tsx`

**Interfaces:**
- Produces: `type UiTheme`, `normalizeUiTheme`, `applyUiTheme`, `readCachedUiTheme`, `fetchUiTheme`, `dispatchUiThemeChanged`, `subscribeUiTheme`, `bootUiTheme`; component `<GlobalUiTheme />`.

- [ ] **Step 1: Write the failing test**

Create `frontend-next/lib/ui-theme.test.ts`:

```ts
// Runs with: node --experimental-strip-types --test lib/ui-theme.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { DEFAULT_UI_THEME, normalizeUiTheme } from "./ui-theme.ts";

describe("normalizeUiTheme", () => {
  it("accepts anthropic", () => assert.equal(normalizeUiTheme("anthropic"), "anthropic"));
  it("accepts elevenlabs", () => assert.equal(normalizeUiTheme("elevenlabs"), "elevenlabs"));
  it("falls back for unknown / null / undefined / non-string", () => {
    assert.equal(normalizeUiTheme("notion"), DEFAULT_UI_THEME);
    assert.equal(normalizeUiTheme(null), DEFAULT_UI_THEME);
    assert.equal(normalizeUiTheme(undefined), DEFAULT_UI_THEME);
    assert.equal(normalizeUiTheme(7), DEFAULT_UI_THEME);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend-next && node --experimental-strip-types --test lib/ui-theme.test.ts`
Expected: FAIL — module `./ui-theme.ts` not found.

- [ ] **Step 3: Create `frontend-next/lib/ui-theme.ts`**

```ts
/* Platform-wide UI theme (SuperAdmin-selected, global). Shared by the web app
 * and the Word add-in via scripts/shared-files.json.
 *
 * The value lives in the system setting `ui.theme`, read from the public
 * endpoint GET /api/v1/system/ui-theme. `data-theme` on <html> selects the
 * Anthropic CSS preset; absence means the built-in ElevenLabs theme.
 * Independent from the per-user light/dark `html.dark` class.
 *
 * No imports: the module must stay loadable by `node --test` (see the test). */

export type UiTheme = "elevenlabs" | "anthropic";

export const UI_THEME_ATTR = "data-theme";
export const UI_THEME_STORAGE_KEY = "weknora_ui_theme";
export const UI_THEME_CHANGED_EVENT = "weknora:ui-theme-changed";
export const UI_THEME_ENDPOINT = "/api/v1/system/ui-theme";
export const DEFAULT_UI_THEME: UiTheme = "elevenlabs";

/** Coerce an arbitrary stored/API value to a known theme. */
export function normalizeUiTheme(value: unknown): UiTheme {
  return value === "anthropic" ? "anthropic" : DEFAULT_UI_THEME;
}

/** Apply a theme to <html> and cache it for the next pre-paint boot. */
export function applyUiTheme(theme: UiTheme): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  if (theme === DEFAULT_UI_THEME) {
    root.removeAttribute(UI_THEME_ATTR);
  } else {
    root.setAttribute(UI_THEME_ATTR, theme);
  }
  try {
    localStorage.setItem(UI_THEME_STORAGE_KEY, theme);
  } catch {
    /* private mode / storage disabled */
  }
}

/** Read the cached theme (used by the pre-paint inline script). */
export function readCachedUiTheme(): UiTheme {
  if (typeof localStorage === "undefined") return DEFAULT_UI_THEME;
  try {
    return normalizeUiTheme(localStorage.getItem(UI_THEME_STORAGE_KEY));
  } catch {
    return DEFAULT_UI_THEME;
  }
}

/** Fetch the authoritative global theme. Never throws. */
export async function fetchUiTheme(timeoutMs = 0): Promise<UiTheme> {
  try {
    const controller = timeoutMs > 0 ? new AbortController() : undefined;
    const timer = controller ? setTimeout(() => controller.abort(), timeoutMs) : undefined;
    const res = await fetch(UI_THEME_ENDPOINT, { cache: "no-store", signal: controller?.signal });
    if (timer) clearTimeout(timer);
    if (!res.ok) return DEFAULT_UI_THEME;
    const body = (await res.json()) as { data?: { theme?: unknown } } | null;
    return normalizeUiTheme(body?.data?.theme);
  } catch {
    return DEFAULT_UI_THEME;
  }
}

/** Notify listeners that the theme changed (admin selector). */
export function dispatchUiThemeChanged(theme: UiTheme): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(UI_THEME_CHANGED_EVENT, { detail: theme }));
}

/** Subscribe to in-page theme changes. Returns an unsubscribe fn. */
export function subscribeUiTheme(listener: (theme: UiTheme) => void): () => void {
  if (typeof window === "undefined") return () => {};
  const handler = (e: Event) => listener(normalizeUiTheme((e as CustomEvent).detail));
  window.addEventListener(UI_THEME_CHANGED_EVENT, handler);
  return () => window.removeEventListener(UI_THEME_CHANGED_EVENT, handler);
}

/** Boot: apply the cached theme now, then reconcile with the server value. */
export async function bootUiTheme(): Promise<UiTheme> {
  applyUiTheme(readCachedUiTheme());
  const theme = await fetchUiTheme();
  applyUiTheme(theme);
  return theme;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend-next && node --experimental-strip-types --test lib/ui-theme.test.ts`
Expected: PASS.

- [ ] **Step 5: Create `frontend-next/components/global-ui-theme.tsx`**

```tsx
"use client";

import { useEffect } from "react";

import { applyUiTheme, bootUiTheme, subscribeUiTheme } from "@/lib/ui-theme";

/* Applies the SuperAdmin-selected global theme at runtime. The pre-paint
 * inline script in app/layout.tsx already set data-theme from cache; this
 * reconciles with the server value and follows in-page changes. */
export function GlobalUiTheme() {
  useEffect(() => {
    void bootUiTheme();
    return subscribeUiTheme(applyUiTheme);
  }, []);
  return null;
}
```

- [ ] **Step 6: Wire the pre-paint script + component in `frontend-next/app/layout.tsx`**

Thêm import và hằng:

```tsx
import { GlobalUiTheme } from "@/components/global-ui-theme";

/* Runs before paint so a returning user never sees the default theme flash.
 * Keep in sync with UI_THEME_STORAGE_KEY / UI_THEME_ATTR in lib/ui-theme.ts. */
const UI_THEME_BOOT_SCRIPT =
  "try{var t=localStorage.getItem('weknora_ui_theme');" +
  "if(t==='anthropic'){document.documentElement.setAttribute('data-theme','anthropic')}}catch(e){}";
```

Trong `<body>`, thêm script + component trước `{children}`:

```tsx
    <body>
      <script dangerouslySetInnerHTML={{ __html: UI_THEME_BOOT_SCRIPT }} />
      <GlobalUiTheme />
      {children}
    </body>
```

- [ ] **Step 7: Verify typecheck**

Run: `cd frontend-next && npm run typecheck`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add frontend-next/lib/ui-theme.ts frontend-next/lib/ui-theme.test.ts frontend-next/components/global-ui-theme.tsx frontend-next/app/layout.tsx
git commit -m "feat(frontend): global ui-theme bootstrap"
```

---

### Task 5: Web — SuperAdmin selector trong Settings → General

**Files:**
- Modify: `frontend-next/lib/api/system.ts`
- Modify: `frontend-next/components/settings/general-settings.tsx`
- Modify: `frontend-next/lib/i18n.tsx`

**Interfaces:**
- Consumes: `getSystemSetting`, `updateSystemSetting` (đã có), `ui-theme` helpers (Task 4).
- Produces: `SYSTEM_UI_THEME_KEY` export; UI chỉ hiện khi `is_system_admin`.

- [ ] **Step 1: Export the setting key in `frontend-next/lib/api/system.ts`**

Cạnh `PARSE_DEFAULTS_SETTING_KEY`:

```ts
/* system_settings key holding the platform-wide UI theme ("elevenlabs" |
 * "anthropic"). SuperAdmin writes it in Settings → General; every user (and
 * the Word add-in) reads the resolved value via GET /system/ui-theme. */
export const SYSTEM_UI_THEME_KEY = "ui.theme";
```

- [ ] **Step 2: Add i18n keys (en + vi)**

Trong `frontend-next/lib/i18n.tsx`, block `en` cạnh `"settings.themeTitle"`:

```ts
  "settings.systemThemeTitle": "System theme",
  "settings.systemThemeDesc": "Applies to every user. Each user still picks light or dark.",
  "settings.systemThemeElevenlabs": "ElevenLabs (default)",
  "settings.systemThemeAnthropic": "Anthropic",
  "settings.systemThemeError": "Could not save the system theme.",
```

Trong block `vi` cạnh `"settings.themeTitle"`:

```ts
  "settings.systemThemeTitle": "Giao diện hệ thống",
  "settings.systemThemeDesc": "Áp dụng cho mọi người dùng. Mỗi người vẫn tự chọn sáng hoặc tối.",
  "settings.systemThemeElevenlabs": "ElevenLabs (mặc định)",
  "settings.systemThemeAnthropic": "Anthropic",
  "settings.systemThemeError": "Không lưu được giao diện hệ thống.",
```

- [ ] **Step 3: Add the selector to `general-settings.tsx`**

Thêm import:

```tsx
import { useAuth } from "@/lib/auth";
import { getSystemSetting, updateSystemSetting, SYSTEM_UI_THEME_KEY } from "@/lib/api/system";
import {
  applyUiTheme,
  dispatchUiThemeChanged,
  normalizeUiTheme,
  type UiTheme,
} from "@/lib/ui-theme";
```

Thêm state + effect trong `GeneralSettings()` (sau state `fontSize`):

```tsx
  const auth = useAuth();
  const isSystemAdmin = auth.user?.is_system_admin === true;
  const [systemTheme, setSystemTheme] = useState<UiTheme>("elevenlabs");
  const [systemThemeError, setSystemThemeError] = useState(false);

  useEffect(() => {
    if (!isSystemAdmin) return;
    getSystemSetting(SYSTEM_UI_THEME_KEY)
      .then((row) => setSystemTheme(normalizeUiTheme(row.value)))
      .catch(() => {
        /* not persisted yet → keep the default */
      });
  }, [isSystemAdmin]);

  const changeSystemTheme = (value: string) => {
    const theme = normalizeUiTheme(value);
    setSystemTheme(theme);
    setSystemThemeError(false);
    applyUiTheme(theme);
    dispatchUiThemeChanged(theme);
    updateSystemSetting(SYSTEM_UI_THEME_KEY, theme).catch(() => setSystemThemeError(true));
  };
```

Thêm khối UI ngay sau khối `{/* theme */}` (chỉ render khi admin):

```tsx
      {/* system theme (SuperAdmin only, global) */}
      {isSystemAdmin && (
        <div className="flex flex-col items-start gap-3 py-4 sm:flex-row sm:items-center sm:justify-between sm:gap-8">
          <div className="min-w-0">
            <label className="title-sm block">{t("settings.systemThemeTitle")}</label>
            <p className="caption mt-1 text-muted">{t("settings.systemThemeDesc")}</p>
            {systemThemeError && (
              <p className="caption mt-1 text-error">{t("settings.systemThemeError")}</p>
            )}
          </div>
          <Select
            className="w-full sm:w-[280px] sm:shrink-0"
            value={systemTheme}
            onChange={changeSystemTheme}
            options={[
              { value: "elevenlabs", label: t("settings.systemThemeElevenlabs") },
              { value: "anthropic", label: t("settings.systemThemeAnthropic") },
            ]}
          />
        </div>
      )}
```

- [ ] **Step 4: Verify typecheck**

Run: `cd frontend-next && npm run typecheck`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend-next/lib/api/system.ts frontend-next/components/settings/general-settings.tsx frontend-next/lib/i18n.tsx
git commit -m "feat(settings): SuperAdmin global theme selector"
```

---

### Task 6: Add-in — token, font, bootstrap

**Files:**
- Modify: `/home/werag-word-addin/scripts/shared-files.json`
- Create: `/home/werag-word-addin/src/styles/theme-anthropic.css` (copy)
- Create: `/home/werag-word-addin/src/lib/ui-theme.ts` (copy)
- Create: `/home/werag-word-addin/src/lib/ui-theme.test.ts` (copy)
- Create: `/home/werag-word-addin/src/anthropic-fonts.css`
- Create: `/home/werag-word-addin/scripts/fetch-anthropic-fonts.mjs`
- Modify: `/home/werag-word-addin/src/styles.css`
- Modify: `/home/werag-word-addin/src/main.tsx`

**Interfaces:**
- Consumes: shared files from Task 3/4.

- [ ] **Step 1: Register the new shared files**

Trong `scripts/shared-files.json`, thêm vào `files`:

```json
    "lib/ui-theme.ts",
    "lib/ui-theme.test.ts",
    "styles/theme-anthropic.css",
```

- [ ] **Step 2: Copy the shared files manually (do NOT run full sync-shared)**

```bash
mkdir -p /home/werag-word-addin/src/styles
cp /home/WeRAG/frontend-next/lib/ui-theme.ts /home/werag-word-addin/src/lib/ui-theme.ts
cp /home/WeRAG/frontend-next/lib/ui-theme.test.ts /home/werag-word-addin/src/lib/ui-theme.test.ts
cp /home/WeRAG/frontend-next/styles/theme-anthropic.css /home/werag-word-addin/src/styles/theme-anthropic.css
```

- [ ] **Step 3: Create the font fetch script**

Create `/home/werag-word-addin/scripts/fetch-anthropic-fonts.mjs`:

```js
// Downloads IBM Plex Sans/Mono woff2 (latin + vietnamese) from Google Fonts
// into public/fonts and writes src/anthropic-fonts.css with /word/ URLs.
// Run: node scripts/fetch-anthropic-fonts.mjs
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const fontsDir = join(root, "public", "fonts");
mkdirSync(fontsDir, { recursive: true });

const cssUrl =
  "https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600;700&family=IBM+Plex+Mono:wght@400;500&display=swap";
const ua =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36";

const css = await (await fetch(cssUrl, { headers: { "User-Agent": ua } })).text();

const blocks = [...css.matchAll(/@font-face\s*\{([^}]*)\}/g)].map((m) => m[1]);
let out = "/* IBM Plex Sans/Mono for the Anthropic theme. Generated by\n * scripts/fetch-anthropic-fonts.mjs — do not edit by hand. */\n";
let n = 0;
for (const block of blocks) {
  const family = /font-family:\s*'([^']+)'/.exec(block)?.[1];
  const weight = /font-weight:\s*(\d+)/.exec(block)?.[1];
  const url = /url\((https:\/\/[^)]+\.woff2)\)/.exec(block)?.[1];
  const range = /unicode-range:\s*([^;]+);/.exec(block)?.[1];
  if (!family || !weight || !url || !range) continue;
  const slug = `${family.toLowerCase().replace(/\s+/g, "-")}-${weight}-${n++}.woff2`;
  const bytes = Buffer.from(await (await fetch(url)).arrayBuffer());
  writeFileSync(join(fontsDir, slug), bytes);
  out += `@font-face {\n  font-family: '${family}';\n  font-style: normal;\n  font-weight: ${weight};\n  font-display: swap;\n  src: url('/word/fonts/${slug}') format('woff2');\n  unicode-range: ${range};\n}\n`;
}
out += "\n:root {\n  --ui-font-sans: 'IBM Plex Sans', 'Segoe UI', Arial, sans-serif;\n  --ui-font-mono: 'IBM Plex Mono', Consolas, monospace;\n}\n";
writeFileSync(join(root, "src", "anthropic-fonts.css"), out);
console.log(`wrote ${n} font files + src/anthropic-fonts.css`);
```

- [ ] **Step 4: Run the font script**

Run: `cd /home/werag-word-addin && node scripts/fetch-anthropic-fonts.mjs`
Expected: `wrote N font files + src/anthropic-fonts.css`; `public/fonts/ibm-plex-*.woff2` xuất hiện.

- [ ] **Step 5: Import in `src/styles.css`**

Đổi các dòng `@import` đầu file:

```css
@import "tailwindcss";
@import "./word-fonts.css";
@import "./anthropic-fonts.css";
@import "./styles/theme-anthropic.css";
```

- [ ] **Step 6: Bootstrap in `src/main.tsx`**

Thay nội dung bằng:

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";
import { WordShell } from "@/components/word/word-shell";
import { Taskpane } from "@/components/word/taskpane";
import { applyUiTheme, bootUiTheme, readCachedUiTheme } from "@/lib/ui-theme";

/* Apply the cached global theme before the first paint, then reconcile with
 * the server value (bounded so a slow backend never blocks the taskpane). */
applyUiTheme(readCachedUiTheme());
const theme = await Promise.race([
  bootUiTheme(),
  new Promise<"elevenlabs">((resolve) => setTimeout(() => resolve("elevenlabs"), 1500)),
]);
applyUiTheme(theme);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <WordShell>
      <Taskpane />
    </WordShell>
  </StrictMode>,
);
```

- [ ] **Step 7: Run add-in checks**

Run: `cd /home/werag-word-addin && npm run typecheck && npm test`
Expected: PASS (gồm `ui-theme.test.ts`).

- [ ] **Step 8: Commit**

```bash
git add scripts/shared-files.json scripts/fetch-anthropic-fonts.mjs src/styles.css src/main.tsx src/anthropic-fonts.css src/styles/theme-anthropic.css src/lib/ui-theme.ts src/lib/ui-theme.test.ts public/fonts
git commit -m "feat(addin): Anthropic theme tokens, fonts and bootstrap"
```

---

### Task 7: Nghiệm thu cuối

- [ ] **Step 1: Backend**

Run: `cd /home/WeRAG && go build ./... && go test ./internal/application/service/ ./internal/handler/ ./internal/router/`
Expected: PASS.

- [ ] **Step 2: Web typecheck + unit test**

Run: `cd /home/WeRAG/frontend-next && npm run typecheck && node --experimental-strip-types --test lib/ui-theme.test.ts`
Expected: PASS.

- [ ] **Step 3: Add-in**

Run: `cd /home/werag-word-addin && npm run typecheck && npm test`
Expected: PASS.

- [ ] **Step 4: Sửa spec cho khớp thực tế test web**

Trong spec `docs/superpowers/specs/2026-10-11-ui-theme-anthropic-design.md` §11.2, đổi `npm run typecheck && npm test` → `npm run typecheck` + `node --experimental-strip-types --test lib/ui-theme.test.ts`.

- [ ] **Step 5: Commit spec fix**

```bash
git add docs/superpowers/specs/2026-10-11-ui-theme-anthropic-design.md
git commit -m "docs(spec): correct web test command"
```

- [ ] **Step 6: detect_changes (nếu tool sẵn có)**

Run: `node .gitnexus/run.cjs detect-changes` (hoặc MCP `detect_changes`) và xác nhận chỉ các symbol dự kiến bị ảnh hưởng.
