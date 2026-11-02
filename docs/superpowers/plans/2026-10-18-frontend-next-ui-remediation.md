# Frontend-next UI Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Triển khai trực tiếp trong chat này; chỉ dùng subagent khi người dùng yêu cầu.

**Goal:** Sửa 15 nhóm vấn đề trong visual audit, ưu tiên khôi phục khả năng xem tài liệu và thao tác trên mobile/laptop nhỏ, giữ phong cách giao diện hiện tại.

**Architecture:** Giữ Next App Router, API và cấu trúc component hiện có. Sửa layout tại component sở hữu; dùng CSS theo chiều rộng container cho grid tài liệu. Gom preference cá nhân và quản lý focus vào các module nhỏ dùng chung; phân biệt chúng với preset theme toàn hệ thống.

**Tech Stack:** Next.js theo package đã cài (`package.json`: `^16.3.5`), React 19, Tailwind CSS 4, TypeScript, `node:test`, BrowserView qua `cua_repl`.

**Spec:** [Báo cáo visual audit, gồm cập nhật sau đăng nhập](../../frontend-next-audit/2026-10-18/REPORT.md). Các quyết định triển khai và tiêu chí nghiệm thu dưới đây làm rõ các phương án trong báo cáo.

> Tiến độ: đã triển khai 7 hạng mục; typecheck, 406 test và build production thành công. Ma trận visual/keyboard/touch chưa hoàn tất do BrowserView từ chối quyền URL. Xem [bản nghiệm thu](../../frontend-next-audit/2026-10-18/VERIFICATION.md).

## Global Constraints

- Chỉ lập kế hoạch ở lượt hiện tại; chưa sửa mã sản phẩm.
- Giữ phong cách ElevenLabs/Anthropic và quyền truy cập hiện hành; không đổi backend/API hoặc luồng nghiệp vụ.
- Giữ nguyên các thay đổi chưa commit có sẵn. Những file đang có thay đổi gồm `doc-workspace.tsx`, `settings-modal.tsx`, `lib/i18n.tsx` và nhiều settings page. Trước khi thực thi phải chụp baseline diff và xác định phần sở hữu; không reset/stash/commit lẫn thay đổi của người dùng.
- Trước mỗi lần sửa symbol, chạy GitNexus `impact({target, direction: "upstream"})`, báo direct callers, affected processes, risk. HIGH/CRITICAL phải thông báo trước khi sửa. Chưa thực hiện impact trong bước lập kế hoạch, nên chưa có kết luận blast radius thực tế.
- Session chưa có GitNexus MCP. Khi triển khai, xác định CLI đang cài hoặc phục hồi runner `.gitnexus/run.cjs`, đọc `--help` để lấy cú pháp thật; không đoán lệnh hoặc thay impact bằng `rg`. Nếu không chạy được, ghi rõ trở ngại trước khi sửa symbol.
- Trước commit, bắt buộc `detect_changes()`; review tổng thể dùng `detect_changes({scope: "compare", base_ref: "main"})`. Chỉ stage các hunk thuộc đợt sửa. Không commit trong giai đoạn lập kế hoạch.
- Đọc hướng dẫn tương ứng trong `frontend-next/node_modules/next/dist/docs/` trước sửa code Next. CSS guide đã được kiểm tra khi lập kế hoạch; khi sửa root layout phải đọc thêm layout/font guide.
- Không thêm thư viện UI hoặc test framework chỉ để sửa CSS. Dùng BrowserView để tái hiện/kiểm tra các thay đổi hiển thị; dùng unit test cho logic trạng thái/preferences mới.
- Không thay đổi preset theme toàn hệ thống để chụp ảnh trên môi trường dùng chung. Dùng môi trường kiểm thử riêng cho ma trận preset; preference cá nhân phải được lưu baseline và phục hồi sau kiểm tra.
- Không gửi tin nhắn, tạo/xóa tài liệu hoặc lưu cấu hình backend chỉ để kiểm tra layout. Dùng dữ liệu hiện có; nếu cần fixture trạng thái khác thì dùng môi trường kiểm thử riêng.
- ONLYOFFICE không tải được là trở ngại môi trường đã quan sát; không sửa endpoint dịch vụ trong đợt CSS này. Không tuyên bố đã kiểm tra nội dung editor nếu iframe chưa tải thành công.

## Review Focus

1. KB có tên/mô tả dài, Wiki bật/tắt và danh sách rỗng: tài liệu vẫn truy cập được — Task 1.
2. Màn hình 320px hoặc landscape, toolbar nhiều tab: menu và nút không bị cắt — Tasks 1–3.
3. Người chọn Dark khi OS Light và ngược lại: token và utility phải cùng chế độ — Task 4.
4. localStorage lỗi, giá trị cũ/sai, System đổi khi app đang mở: preference hợp lệ và không crash — Task 5.
5. Drawer đóng, modal lồng, trigger bị unmount: focus không chạy ra nền hoặc vào nội dung ẩn — Task 6.

## Cách chia đợt

| Đợt | Task | Kết quả độc lập |
| --- | --- | --- |
| A | 1–3 | Xem và thao tác được ở mobile/laptop nhỏ |
| B | 4–5 | Màu và preference hoạt động thống nhất |
| C | 6–7 | Bàn phím, typography và tài liệu kiểm chứng nhất quán |

Mỗi task phải qua kiểm tra riêng trước khi chuyển tiếp. Lịch thực thi mặc định tuần tự để tránh xung đột trên CSS, i18n và các file đang được chỉnh dở.

### Task 1: Khôi phục bố cục kho tri thức và trạng thái Wiki

**Files:** Modify `frontend-next/components/kb-detail.tsx`, `frontend-next/components/wiki/wiki-browser.tsx`, `frontend-next/lib/i18n.tsx`; Create `frontend-next/components/knowledge/kb-detail-layout.module.css`. Chỉ mở rộng `lib/api/knowledge.ts` nếu payload thật thiếu type cần thiết, không đổi request.

**Interfaces:** Giữ `KbDetail({kbId})` và `WikiBrowser({kbId, changed?, q?, canMutate?})`. Dùng `KnowledgeBaseRow.indexing_strategy?.wiki_enabled`: chỉ `false` rõ ràng mới xác nhận Wiki tắt; thiếu field không được tự suy ra tắt.

- [ ] Chạy impact cho `KbDetail`, `WikiBrowser` và các symbol i18n thực sự phải sửa; lưu kết quả vào bằng chứng task.
- [ ] Tái hiện baseline KB có 20 tài liệu tại 390×844 và 1024×768: lưu ảnh, `clientHeight` grid và bounding box mép phải. Baseline đã thấy grid mobile = 0px và desktop right = 1057.92px.
- [ ] Sửa mobile dưới 1024px thành một vùng nội dung cuộn dọc: tài liệu trước, Wiki sau; grid theo chiều cao nội dung, không giữ Wiki `45vh` chiếm phần chiều cao còn lại. Header/action bar wrap, tabs cho phép cuộn ngang riêng với nhãn không bị ép thành nhiều dòng.
- [ ] Desktop từ 1024px: Wiki co giãn trong khoảng 240–320px và tối đa 35% vùng nội dung; section tài liệu `min-width:0`. Grid dùng `repeat(auto-fit,minmax(min(100%,240px),1fr))`, không quyết định ba cột bằng viewport `lg`. Giữ vùng cuộn riêng trên desktop khi đủ chiều cao.
- [ ] Khi Wiki tắt rõ ràng, bỏ pane lớn và không gọi Wiki index; chỉ hiện thông tin ngắn khi cần. Trong `WikiBrowser`, loading/error/empty phải loại trừ nhau, không hiển thị raw error cùng empty message. Với trạng thái chưa rõ, giữ khả năng tải và retry; không suy đoán từ chuỗi lỗi.
- [ ] BrowserView kiểm tra: 320×568, 390×844, 844×390, 768×1024, 1024×768, 1280×800, 1440×900; KB Wiki bật/tắt, tiêu đề dài và rỗng. Đạt khi tài liệu nhìn/cuộn được, card rộng ít nhất 240px khi có đủ chỗ, mọi action truy cập được, không nội dung bị cắt ngoài viewport.
- [ ] Chạy `npm run typecheck` từ `frontend-next`; xem diff task. Ghi ảnh/số đo sau sửa, chỉ đánh dấu task hoàn tất khi baseline lỗi đã hết.

### Task 2: Giữ menu tài liệu và header preview trong màn hình

**Files:** Modify `frontend-next/components/doc-workspace/doc-workspace.tsx`, `frontend-next/components/doc-preview-modal.tsx`; Create `frontend-next/components/doc-workspace/document-add-menu.tsx` nếu cần portal để thoát ancestor clipping.

**Interfaces:** Giữ handler upload/create hiện tại. Component menu mới nếu dùng: `DocumentAddMenu({anchorRef: RefObject<HTMLButtonElement | null>, open: boolean, onClose: () => void, children: ReactNode})`; không sở hữu nghiệp vụ hoặc API.

- [ ] Impact `DocWorkspace`, `DocPreviewModal`; đọc lại diff có sẵn của workspace trước khi sửa.
- [ ] Tái hiện menu tại 390px: baseline left=105.5, right=425.5, width=320. Kiểm tra thêm khi toolbar có nhiều tab và tại desktop split pane.
- [ ] Đặt menu trong portal lên body nếu ancestor clip; dùng fixed position theo nút mở, width tối đa `min(320px, viewportWidth - 16px)`, clamp left trong `[8px, viewportWidth - width - 8px]`. Đóng khi click ngoài, Escape, resize hoặc scroll vùng chứa; không thay thứ tự hay hành vi action.
- [ ] Preview dưới `sm`: tên tệp ở hàng đầu có `min-w-0`, action ở hàng thứ hai; nút đóng `shrink-0` và có `aria-label`. Tên dài được xuống tối đa hai dòng, không bị ép chỉ còn một ký tự.
- [ ] Kiểm tra 320/390px và desktop split, mở/đóng menu bằng chuột/bàn phím, resize khi mở. Đạt khi menu cách mép ít nhất 8px, không bị ancestor cắt, trigger nhận lại focus khi đóng bằng Escape.
- [ ] Typecheck, ảnh before/after và review đúng hunk; không bấm upload/create/save để xác minh layout.

### Task 3: Cân lại card thành phẩm và header/settings nhỏ

**Files:** Modify `frontend-next/app/platform/artifacts/page.tsx`, `frontend-next/components/settings/storage-settings.tsx`, `frontend-next/components/settings/vector-store-settings.tsx`, `frontend-next/components/settings/input-limits-card.tsx`.

**Interfaces:** Giữ props, filter, tab, preview và request hiện có; không tạo abstraction chung cho vài header đơn giản.

- [ ] Impact `ArtifactsInner`, `StorageSettings`, `VectorStoreSettings`, `InputLimitsCard`.
- [ ] Chụp baseline thành phẩm mobile có tệp thật và header storage/vector. Tái hiện input limits trong modal 320px trước khi kết luận cần sửa nơi nào.
- [ ] Thành phẩm: một cột mặc định, hai cột từ `sm`, ba từ `lg`, bốn từ `xl` khi vùng nội dung đủ; search `w-full sm:w-[280px]`. Giữ tên action đầy đủ; không dùng truncate để che lỗi chiều rộng action.
- [ ] Header storage/vector dùng `flex-col items-start gap-3` trên mobile, trở lại hàng ngang từ `sm`; mô tả `min-w-0`, action `shrink-0`. Các nhóm input limits dùng `min-w-0 w-full basis-[260px] flex-1` để xuống hàng nhưng vẫn co được dưới 260px.
- [ ] Kiểm tra settings ở 320/390/640/768px, modal nội dung dài, tên backend dài; thành phẩm 1 và nhiều tệp. Đạt khi đọc đủ “Xem trước”/“Tải xuống”, không có cuộn ngang toàn modal và không mô tả bị bó cạnh nút.
- [ ] Typecheck và so ảnh desktop để bảo đảm card không trở nên quá thưa; không thêm test snapshot chuỗi class.

### Task 4: Đồng bộ dark variant và sửa token màu

**Files:** Modify `frontend-next/app/globals.css`, `frontend-next/styles/theme-anthropic.css` nếu cần; các component gọi utility thiếu token do quét nguồn xác định, ưu tiên skills, memory, browser connection, invitations, env vars, preview và choice list.

**Interfaces:** Giữ tên token chuẩn `surface-card`, `primary`, `on-primary`, `ink`, `muted`. Preset dùng `data-theme`; light/dark dùng `.dark`.

- [ ] Lập danh sách consumer của token/CSS; impact từng component/symbol trước khi sửa. Báo phạm vi toàn frontend của CSS global, không gán mức risk GitNexus khi chưa có kết quả.
- [ ] Ghi baseline CSS biên dịch: `dark:*` đang nằm trong media query OS; ElevenLabs dark primary/background và foreground cùng màu.
- [ ] Thêm `@custom-variant dark (&:where(.dark, .dark *));` cho Tailwind 4; đặt `--color-on-primary: #171412` ở ElevenLabs dark.
- [ ] Thay từng utility chưa tồn tại theo ngữ nghĩa: surface → surface-card; brand/accent foreground → primary hoặc ink; selected background → primary cùng on-primary. Với `bg-brand/10`, dùng primary/10 và foreground primary; không thay `text-white` toàn cục vì một số nút danger phải giữ trắng.
- [ ] Quét lại để không còn utility thiếu token đã báo cáo; kiểm tra stylesheet thực sự sinh selector. Không bổ sung alias mơ hồ chỉ để làm quét hết cảnh báo.
- [ ] Trên môi trường test riêng, kiểm tra 2 preset × 3 mode (Light/Dark/System) × 2 OS preference. Đạt khi `.dark` quyết định cả token/utility và chữ nút primary tương phản ít nhất 4.5:1 ở trạng thái thường/hover. Kiểm tra chat bubble, toggle, tab tài liệu, nút chọn file skills và preview.
- [ ] Typecheck, review CSS cascade, lưu ảnh light/dark. Không sửa hoặc đồng bộ sang repository Word add-in trong đợt này; ghi dependency ngoài repo nếu phát hiện.

### Task 5: Làm preference cỡ chữ/theme hoạt động và phục hồi sau reload

**Files:** Create `frontend-next/lib/ui-preferences.ts`, `frontend-next/lib/ui-preferences.test.ts`; Modify `frontend-next/components/global-ui-theme.tsx`, `frontend-next/components/settings/general-settings.tsx`, `frontend-next/app/layout.tsx`, `frontend-next/app/globals.css` và các consumer typography được quét.

**Interfaces:** Module không phụ thuộc React: `ThemeMode = "light" | "dark" | "system"`, `FontSizeKey = "small" | "normal" | "large"`; `normalizeThemeMode(value: unknown): ThemeMode`, `normalizeFontSize(value: unknown): FontSizeKey`, `resolveDarkMode(mode: ThemeMode, systemDark: boolean): boolean`, `fontScale(size: FontSizeKey): number`. Giữ storage keys `weknora_theme`, `weknora_font_size`; scale `0.9`, `1`, `1.12`.

- [ ] Impact `GeneralSettings`, `applyTheme`, `GlobalUiTheme`, `RootLayout`; không thay `lib/ui-theme.ts` nếu không cần vì module đó sở hữu preset toàn hệ thống.
- [ ] Viết unit test: invalid/null → System/normal; Dark luôn true, Light luôn false, System theo OS; ba scale chính xác. Chạy `node --experimental-strip-types --test lib/ui-preferences.test.ts`, xác nhận fail trước implementation.
- [ ] Implement module và wiring: bootstrap trước paint phục hồi `.dark` và `--app-font-scale` theo stored preference; effect trong global component đăng ký media-query listener, chỉ áp dụng OS change khi mode System, cleanup đúng. Settings dùng cùng normalization và cập nhật DOM khi người dùng chọn.
- [ ] Chuyển đơn vị biến scale sang dạng số; typography dùng `calc(<base-size> * var(--app-font-scale, 1))`. Body/display/title/caption/button/input và text chính ở các màn hình audit phải thực sự thay đổi. Quét `text-[Npx]`/inline font-size; chuyển consumer chữ sang cùng cơ chế, không scale SVG, kích thước canvas hoặc nội dung editor nhúng.
- [ ] Với text overlay mention của composer, foreground và textarea phải dùng cùng font-size/line-height để không lệch vị trí. Form nhập trên thiết bị cảm ứng giữ computed font-size tối thiểu 16px; không dùng CSS zoom cho cả trang.
- [ ] Chạy unit test green + `lib/ui-theme.test.ts` + typecheck. BrowserView kiểm tra Small/Normal/Large, reload, storage không hợp lệ/bị chặn trong môi trường test, System đổi khi đang mở. Font Large phải tăng 12% so với base, không chỉ đổi nút active. Phục hồi preference người dùng sau test.

### Task 6: Focus modal/drawer và khả năng đọc/thao tác

**Files:** Create `frontend-next/components/use-dialog-focus.ts`; Modify `frontend-next/components/modal.tsx`, `frontend-next/components/slide-panel.tsx`, `frontend-next/components/settings-modal.tsx`, `frontend-next/components/doc-preview-modal.tsx`, `frontend-next/components/composer.tsx` và CSS nút/caption liên quan.

**Interfaces:** `useDialogFocus({open: boolean, containerRef: RefObject<HTMLElement | null>}): void`. Hook quản lý focus và thứ tự dialog đang mở; host sub-page không tự tạo focus trap thứ hai. Không tách hoặc thay API `Modal`/`SlidePanel`.

- [ ] Impact các component overlay và composer; rà soát hosted modal/sub-page để chỉ áp dụng hook cho overlay thật.
- [ ] Tái hiện Tab/Shift+Tab vào nền và drawer đóng trước khi sửa. Mở đồng thời settings và sub-page/menu để ghi hành vi hiện có.
- [ ] Hook nhớ activeElement, focus phần tử tương tác đầu tiên khi mở, chặn Tab trong dialog trên cùng, phục hồi trigger còn connected khi đóng. Portal mount trễ phải nhận focus sau mount; không focus vào node ẩn/disabled. Nếu trigger mất, dùng fallback khả dụng, không gọi focus node đã unmount.
- [ ] Drawer đóng dùng `inert` và `aria-hidden`; overlay có `aria-modal=true` khi mở. Không bẫy focus của popup portal phía trên hoặc đóng đồng thời hai lớp bằng một Escape. Bổ sung tên truy cập cho icon button còn thiếu.
- [ ] Nội dung caption cần đọc chuyển từ muted-soft sang muted theo từng màn hình; giữ muted-soft cho trang trí. Với pointer coarse, vùng bấm action quan trọng ít nhất 44×44px; toolbar được wrap trước khi tăng hit area để tránh chồng lấn. Không phóng to mọi nút desktop.
- [ ] BrowserView kiểm tra keyboard loop, Escape, nested overlays, trigger unmount, drawer đóng, 320px/112% font. Kiểm tra DOM focus/inert và ảnh; typecheck. Không viết test giả lập DOM sơ sài chỉ để khẳng định hook chạy.

### Task 7: Đồng bộ trang mẫu, i18n và nghiệm thu toàn đợt

**Files:** Modify `frontend-next/app/design/page.tsx`, `frontend-next/lib/i18n.tsx`, nhãn hardcode trong các component đã sửa; Create `docs/frontend-next-audit/2026-10-18/VERIFICATION.md` và thư mục ảnh `after/`.

**Interfaces:** Không thay public interface; đọc computed CSS của mẫu để hiển thị font-size/color/font-family thực tế. Nhãn mới thêm cả `en` và `vi` theo mẫu i18n sẵn có.

- [ ] Impact `DesignPage` và các symbol cần sửa. Loại nhãn type scale và mã swatch hardcode mâu thuẫn; trang mẫu hiển thị preset đang áp dụng và computed typography sau resize/theme/font-scale change.
- [ ] Mẫu chữ tiếng Việt có dấu, tên tài liệu dài; mobile chuyển dòng mẫu/caption thành cột và giảm padding. Hoàn thiện nhãn Việt/Anh trong các vùng đã sửa, không dịch toàn bộ repository ngoài phạm vi.
- [ ] Lập bảng 15 findings → task → ảnh/test sau sửa. Mục chưa tái hiện hoặc bị dịch vụ chặn phải ghi rõ, không tự đánh dấu pass.
- [ ] Chạy `npm run typecheck`; unit tests mới và các test hiện có liên quan. Build trên môi trường test với `WERAG_BACKEND_URL` lấy từ cấu hình thực tế, không tự đoán endpoint. Không build đè `.next` của dev server đang được người dùng sử dụng; dùng checkout/instance kiểm thử riêng.
- [ ] BrowserView nghiệm thu lại KB list/detail/create, new chat/existing chat, document toolbar/preview, artifacts, settings/admin trên desktop/mobile; thêm login/register/onboarding cho regression CSS global. Embed/Word login chỉ xác minh luồng có fixture hợp lệ, ghi rõ phần còn thiếu.
- [ ] Ghi before/after, viewport, computed dimensions, command và exit code vào `VERIFICATION.md`; khôi phục viewport/preference. Tài liệu mobile phải có vùng hiển thị thực, không chỉ `scrollHeight>0` trong phần tử cao 0px.
- [ ] GitNexus detect_changes và review diff cuối cùng, đối chiếu baseline thay đổi có sẵn. Nếu tạo commit, chỉ stage hunk đợt sửa sau khi hoàn tất các kiểm tra. Báo kết quả, giới hạn ONLYOFFICE/fixture và ảnh cuối cho người dùng.

## Phủ yêu cầu audit

| Finding | Task |
| --- | --- |
| 1 dark variant; 2 primary dark; 3 token thiếu | 4 |
| 4 grid desktop; 12 grid mobile; 15 Wiki state | 1 |
| 5 preference/theme/cỡ chữ | 5 |
| 6 menu thêm văn bản | 2 |
| 7 minimum width settings | 3 |
| 8 contrast/touch target | 5–6 |
| 9 focus overlay | 6 |
| 10–11 design system/type scale | 7 |
| 13 thành phẩm mobile | 3 |
| 14 header settings/preview | 2–3 |

## Điều kiện hoàn tất

Hoàn thành khi 15 nhóm có kết quả xác minh hoặc giới hạn cụ thể được báo lại; không còn lỗi tài liệu mobile cao 0px, grid/menu tràn, primary dark mất chữ hoặc nhãn thao tác bị ép mất nghĩa. Mọi thay đổi global đã qua ma trận theme và font; typecheck/tests liên quan thành công; thay đổi người dùng được bảo toàn. Kế hoạch này chưa phải bằng chứng các lỗi đã được sửa.
