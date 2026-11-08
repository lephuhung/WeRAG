# Theo dõi usage, chi phí LLM và prompt cache

Tài liệu vận hành: đọc số liệu ở đâu, cách tính tiền thật của một gói, và cách làm
cho provider cache trúng nhiều nhất.

Liên quan: `docs/agent-prompt-assembly.md` (thứ tự các section prompt — quyết định
prefix có ổn định hay không), `internal/models/pricing/pricing.go` (bảng giá),
`internal/types/usage.go` (định nghĩa metric).

---

## 1. Vì sao không thể tính tiền bằng "số token × một giá"

Bảng giá DeepSeek (per 1M token, nguồn `https://api-docs.deepseek.com/quick_start/pricing`):

| | deepseek-flash | deepseek-v4-pro |
|---|---|---|
| input, cache **hit** (off-peak / peak) | $0.003 / $0.006 | $0.022 / $0.044 |
| input, cache **miss** (off-peak / peak) | $0.15 / $0.30 | $0.66 / $1.32 |
| **output** (off-peak / peak) | $0.60 / $1.20 | $1.98 / $3.96 |

Ba hệ số chênh lệch cùng lúc:

1. **Cache hit vs miss: 50×** ($0.006 vs $0.30). Cùng một `prompt_tokens` cho ra
   hai hoá đơn khác nhau 50 lần tuỳ vào cache.
2. **Peak vs off-peak: 2×**. Peak là 01:00–04:00 và 06:00–10:00 UTC, T2–T6 →
   **giờ Việt Nam là 08:00–11:00 và 13:00–17:00**, tức đúng giờ khách chat. Gần
   như toàn bộ traffic thật bị tính giá peak.
3. **Model: 3.3×** (flash vs v4-pro).

Cộng thêm: **thinking mode tính như output**. Một câu trả lời hiển thị 400 token
có thể tốn 1.500–3.000 token output. Số `completion_tokens` đã bao gồm phần này
nên không cần sửa công thức, chỉ cần biết vì sao hoá đơn cao hơn "độ dài câu trả lời".

Hệ quả khi đặt giá gói: nếu tính bằng "tokens × giá trung bình", sai số 2–10 lần.
Đó là lý do phần còn lại của tài liệu đo cả **cache** và **tiền**, chứ không chỉ token.

---

## 2. DeepSeek cache hoạt động thế nào (và cách nó trượt)

Nguồn: `https://api-docs.deepseek.com/guides/kv_cache`. Tóm tắt đúng phần ảnh
hưởng đến thiết kế request:

- **Tự động, không cần tham số.** Không có `cache_control`, không có
  `prompt_cache_key`. (Code đã đúng: `promptCachePolicyFor` chỉ gửi các field đó
  cho OpenAI; DeepSeek không nhận gì thêm.)
- Cache nằm trên **disk**, đơn vị là **cache prefix unit** — một **tiền tố hoàn
  chỉnh**. Request sau chỉ trúng khi **khớp toàn bộ** một unit đã có.
- Unit được tạo tại: (a) **cuối input người dùng** và **cuối output của model**
  của mỗi request; (b) khi hệ thống phát hiện **tiền tố chung** giữa nhiều request;
  (c) các mốc token cố định trong input/output dài.
- Hệ quả then chốt: hội thoại nhiều lượt dạng **append** (`A+B` → `A+B+C` →
  `A+B+C+D`) trúng cache gần như toàn bộ. Còn `A+B` → `A+C` **không trúng** unit
  `A+B`, và cũng chưa trúng `A` ngay — `A` phải được persist thành unit riêng
  (rule b) rồi request thứ ba mới trúng.
- Cache **best-effort**, không đảm bảo 100%; TTL thực tế vài giờ đến vài ngày.

### Nguyên tắc rút ra cho request của WeRAG

| Quy tắc | Lý do |
|---|---|
| System prompt **byte-identical** giữa các lượt trong một phiên | Nó là đầu tiên trong prefix; đổi 1 byte là mất toàn bộ phần sau |
| **Không** chèn thời gian/ID ngẫu nhiên vào system prompt | Làm mới mỗi lượt = hit rate 0. Code hiện đặt `<current_time>` (chỉ granularity **ngày**) trong **message user cuối**, không phải system — đúng. Lưu ý: `{{current_time}}` **trong template system prompt tự soạn** cũng làm prefix đổi mỗi ngày (1 lần miss/ngày, chấp nhận được nhưng nên biết) |
| **Không** đặt thứ recall theo query vào system prompt | Memory dài hạn và legal catalogue đã được chuyển sang lượt user (xem "Đã sửa hình dạng request" bên dưới) |
| Context RAG dễ đổi (danh sách KB, tài liệu ghim, scope) đặt ở **message cuối** | Nằm sau prefix ổn định thì phần ổn định vẫn trúng |
| Bộ **tool schema** giữ nguyên giữa các lượt | Tool nằm cùng vùng prefix; lọc tool theo lượt là đổi prefix |
| History **chỉ append**, không rewrite | Sửa/đảo thứ tự tin nhắn cũ = đổi prefix = miss |
| Compaction/summarize chấp nhận 1 lần miss | Nó viết lại phần đầu; các lượt sau lại trúng trên prefix mới |
| Ít nhất 1 request "làm nóng" mỗi prefix | Request đầu luôn miss (unit chưa tồn tại) |

### Trạng thái trong code

- `internal/models/chat/prompt_cache.go` — `applyRawPromptCacheUsage` đọc
  `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` (DeepSeek),
  `cache_read_input_tokens` (Anthropic), `cached_tokens` ở root (Moonshot/Kimi)
  và `prompt_tokens_details.cached_tokens`.
- `cacheAccountingOutOfBand` + `buildOutbound`: endpoint dạng DeepSeek **buộc đi
  đường raw HTTP**. Lý do: SDK `go-openai` chỉ decode `prompt_tokens_details`,
  nên hai field `prompt_cache_*_tokens` bị nuốt mất. Trước thay đổi này, mọi lượt
  DeepSeek đi qua SDK báo `cache_read_tokens = 0` / `cache_status=unsupported`,
  và báo cáo chi phí đọc token cache theo **giá miss** → đắt lên tới 50×.
- `PromptPrefixFingerprint` / `BuildPromptCacheKey` chỉ phục vụ **log và đối
  chiếu**, không gửi cho DeepSeek.
- `CacheRetention` (`none`/`short`/`long`) chỉ có tác dụng với provider hiểu
  `cache_control` / `prompt_cache_key`.

### Đã sửa hình dạng request để cache dùng được

Ba thay đổi, tất cả theo cùng một nguyên tắc: **thứ đổi theo từng lượt phải nằm
SAU history, trong lượt của người dùng**.

1. **Memory dài hạn ra khỏi system prompt** (`internal/agent/prompts.go`,
   `observe.go`). Envelope `<user_memory>` trước đây là section cuối của system
   prompt, mà nội dung của nó được recall **theo câu hỏi** (facts/tasks + interest
   khớp query). Đổi một byte ở system message = mất cache của **toàn bộ history**
   phía sau → hit rate gần 0 ở mọi lượt có memory bật. Nay nó nằm trong
   `RenderUserTurnContent` (runtime_context → memory → must_use → query).
2. **Memory + legal catalogue ra khỏi system prompt ở pipeline RAG**
   (`chat_pipeline/common.go`). Cùng lý do: `LegalCatalogPrompt` cũng build theo
   query. `UserContextPrompt` (hồ sơ người dùng: đơn vị, tỉnh) **giữ nguyên**
   trong system prompt vì nó không đổi giữa các lượt.
3. **DeepSeek đi raw HTTP** (mục trên) để con số hit rate phản ánh đúng thực tế
   — nếu không thì có tối ưu cũng không đo được.

Hai test giữ cho điều này không tái phát:
`internal/agent/engine_memory_test.go` (`TestSystemPromptIsStableAcrossTurns…`)
và `internal/application/service/chat_pipeline/prompt_stability_test.go`
(so `prompt_prefix` của hai lượt khác câu hỏi/khác memory — phải giống nhau).

**Chỗ còn có thể làm mất cache** (biết để kiểm tra khi hit rate tụt):

- `{{contexts}}` hoặc `{{query}}` trong **template system prompt tự soạn** — mặc
  định không có (context đã đi vào `UserContent`), nhưng nếu ai thêm vào thì
  mọi lượt đều miss.
- `{{current_time}}` trong template tự soạn → miss 1 lần/ngày.
- Bộ tool đổi giữa các lượt (đổi scope KB, bật/tắt graph, @mention MCP/skill) →
  phần `sources`/`tools` của system prompt đổi → miss.
- Compaction/`HistorySummary` viết lại phần đầu history → miss đúng lượt đó, các
  lượt sau trúng lại trên prefix mới.
- Provider khác (OpenAI) vẫn cần `prompt_cache_key` + `cache_control`; DeepSeek
  không nhận hai thứ đó (gửi vào là lỗi 400).
- Đường "trả lời bằng kiến thức chung khi KB không khớp"
  (`session_knowledge_qa.go`) vẫn để memory trong system message: cả message đó
  đã per-turn (danh sách tài liệu + ngày hiện tại) nên cache không dùng được ở
  đó — không cần sửa, chỉ đừng copy cách đó sang đường chính.

---

## 3. Số liệu được ghi ở đâu

Mỗi lượt trả lời ghi vào `usage_counters` theo (scope, metric, period):

| metric | ý nghĩa |
|---|---|
| `questions` | số lượt hỏi |
| `tokens` | `total_tokens` của lượt |
| `prompt_tokens` / `completion_tokens` | tách input/output (output **gồm** token suy luận) |
| `cache_read_tokens` | phần input provider phục vụ từ prompt cache |
| `cache_miss_tokens` | phần input còn lại |
| `cost_micro_vnd` | **tiền**, đơn vị 1/1.000.000 đồng |
| `ocr_tokens` / `ocr_pages` | token đọc ảnh/trang scan |

Điểm quan trọng:

- **Tiền chốt tại thời điểm gọi**, dùng đúng giờ (peak/off-peak) và đúng model
  của lượt đó (`internal/models/pricing`). Đổi giá sau này **không** viết lại
  lịch sử — mỗi lượt giữ giá nó đã bị tính. Rate USD→VND lấy từ `PRICING_USD_VND`
  (mặc định 26.300), đọc một lần lúc khởi động để cả tháng dùng một tỷ giá.
- **Model không có trong bảng giá** → `cost_micro_vnd` không tăng (token vẫn
  tăng). Đây là tín hiệu để phát hiện model mới chưa được khai giá, thay vì lấy
  một giá đoán ra.
- **Provider không báo cache** → cả `cache_read_tokens` và `cache_miss_tokens`
  **không có row**. "Không có row" = mù thông tin, khác hẳn "trúng 0%". Đừng
  `COALESCE` thành 0 khi làm báo cáo mà bỏ mất khác biệt này.
- `cost_micro_vnd` là chi phí của **operator**, không trả về cho tenant: field
  `types.TokenUsage.CostMicroVND` mang `json:"-"` để không rò qua API/message/stream.
- SuperAdmin xem ở `GET /system/admin/stats` → `data.usage.today|month`, đã có
  sẵn `cache_read_tokens`, `cache_miss_tokens`, `cost_micro_vnd`; dashboard hiển
  thị thẻ "Chi phí LLM (tháng này)".

Log từng lượt (grep được, không cần DB):

```
[LLM Usage] model=deepseek-v4-pro, purpose=agent, prompt_prefix=ab12…,
  prompt_tokens=18234, completion_tokens=912, cache_read_tokens=17408,
  cache_miss_tokens=826, cache_hit_rate=95.5%, cache_reported=true, cache_status=hit,
  price=deepseek-v4-pro, price_tier=peak, cost_micro_vnd=4548700, session_id=…
```

---

## 4. Truy vấn vận hành

Hit rate + chi phí theo tổ chức, tháng hiện tại:

```sql
SELECT scope_id AS tenant_id,
       MAX(value) FILTER (WHERE metric = 'tokens')              AS tokens,
       MAX(value) FILTER (WHERE metric = 'cache_read_tokens')   AS cache_read,
       MAX(value) FILTER (WHERE metric = 'cache_miss_tokens')   AS cache_miss,
       ROUND(100.0 * MAX(value) FILTER (WHERE metric = 'cache_read_tokens')
             / NULLIF(MAX(value) FILTER (WHERE metric = 'cache_read_tokens')
                    + MAX(value) FILTER (WHERE metric = 'cache_miss_tokens'), 0), 1) AS hit_pct,
       ROUND(MAX(value) FILTER (WHERE metric = 'cost_micro_vnd') / 1e6) AS cost_vnd
FROM usage_counters
WHERE scope_type = 'tenant' AND period_key = to_char(now(), 'YYYY-MM')
GROUP BY scope_id
ORDER BY cost_vnd DESC NULLS LAST;
```

Chuỗi ngày của toàn hệ thống (phát hiện ngày chi phí vọt):

```sql
SELECT period_key AS day,
       SUM(value) FILTER (WHERE metric = 'tokens')            AS tokens,
       ROUND(SUM(value) FILTER (WHERE metric = 'cost_micro_vnd') / 1e6) AS cost_vnd
FROM usage_counters
WHERE scope_type = 'tenant' AND metric IN ('tokens', 'cost_micro_vnd')
  AND period_key ~ '^\d{4}-\d{2}-\d{2}$'
GROUP BY 1 ORDER BY 1 DESC LIMIT 30;
```

Tính chi phí cho giai đoạn **trước** khi có `cost_micro_vnd` (backfill chỉ có
token + cache), dùng giá peak cho an toàn:

```sql
-- ví dụ deepseek-flash: hit 0.006, miss 0.30, output 1.20 (USD/1M) × 26300
cost_vnd ≈ (
    cache_read  * 0.006
  + cache_miss  * 0.30
  + (tokens - prompt_tokens) * 1.20
) * 26300 / 1e6
```

---

## 5. Từ số liệu đến giá gói

1. Chạy 1–2 ngày traffic đại diện (hoặc dùng tháng trước).
2. Lấy `S = cost_micro_vnd / questions` → **chi phí LLM trung bình mỗi lượt hỏi**.
   Đây là con số duy nhất cần cho mọi gói.
3. Mỗi gói có `L` lượt/tháng (hoặc `T` token/tháng) → chi phí biên:
   `C = S × L` (hoặc `T × giá/mỗi token` từ cùng bộ số).
4. Giá gói: `P ≥ (C × (1 + buffer) + hạ tầng) / (1 − margin_mục tiêu)`.
   `buffer` 20–30% cho: peak cao hơn trung bình, thinking bật nhiều hơn dự kiến,
   retry, và các call không phải chat (đặt tên phiên, OCR, agent tool rounds).
5. Khi đổi model (pro → flash) hoặc tắt thinking, `S` đổi ngay trong báo cáo
   ngày — dùng chính bảng trên để xác nhận gói mới có lãi.

Ví dụ với bảng giá hiện tại (peak, đ/1M token, tỷ giá 26.300):

| | flash | v4-pro |
|---|---|---|
| 1M output | 31.560đ | 104.148đ |
| 1M input (miss) | 7.890đ | 34.716đ |
| 1M input (hit) | 158đ | 1.157đ |
| 1 lượt hỏi RAG 5:1, hit 90% | ≈ 36đ | ≈ 127đ |
| 1 lượt hỏi RAG 10:1, hit 90% | ≈ 41đ | ≈ 149đ |

(Dòng "1 lượt" = 1.000 output + 5.000/10.000 input, 90% input trúng cache. Cùng
lượt đó nếu hit 0% thì đắt thêm 4–10 lần — hit rate là biến quan trọng nhất, hơn
cả việc chọn model.)

---

## 6. Checklist hằng tháng

- [ ] `GET /system/admin/stats` — so `cost_micro_vnd` với doanh thu kỳ đó.
- [ ] Hit rate toàn hệ thống: dưới ~70% là bất thường, xem mục 2 (prompt/system
  prompt bị đổi giữa phiên? tool set dao động? compaction quá thường?).
- [ ] Có dòng `price=unpriced` trong log → model chưa có giá trong
  `internal/models/pricing/pricing.go`.
- [ ] Đối chiếu bảng giá với trang pricing của provider; nếu đổi, sửa bảng + test
  `TestFromUsageMillionOutputTokens` (số cứng cố ý, để lệch là fail).
- [ ] Nếu tỷ giá USD/VND lệch > 2%, cập nhật `PRICING_USD_VND` (áp dụng từ lần
  khởi động sau; số liệu cũ giữ tỷ giá cũ — đúng như mong muốn khi quyết toán).
