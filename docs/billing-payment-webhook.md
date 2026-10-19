# Thanh toán tự động: webhook nhận giao dịch

Tài liệu cho người vận hành (bật, kiểm tra, xử lý hằng ngày) và cho người viết
dịch vụ tự kiểm tra giao dịch ngân hàng.

## Cách hoạt động

```
Người mua tạo đơn ──► trang thanh toán hiện VietQR, nội dung CK = mã đơn WRxxxxxxxx
                             │
                    người mua chuyển khoản
                             │
Dịch vụ kiểm tra giao dịch (đọc sao kê / SePay / Casso …)
                             │  POST /api/v1/billing/payments/webhook  (mỗi giao dịch một request)
                             ▼
WeRAG lưu giao dịch ──► nội dung có mã đơn đang chờ + đủ tiền?
                             ├── có  → đơn "đã thanh toán", gói được kích hoạt/gia hạn ngay
                             └── không → giao dịch nằm lại với trạng thái + lý do, SuperAdmin xử lý tay
```

- Dịch vụ kiểm tra là **thành phần riêng**, chỉ đẩy dữ liệu; WeRAG không gọi ngân hàng.
- Gửi lại cùng một giao dịch không bao giờ tính tiền hai lần.
- Cách xác nhận tay cũ (SuperAdmin bấm "Xác nhận" ở trang
  **Hệ thống → Thanh toán & khuyến mãi**, `/platform/system/admin/billing`) vẫn dùng được song song.

---

## 1. Bật lần đầu

Mọi bước làm trên trang **Hệ thống → Thanh toán & khuyến mãi**
(`/platform/system/admin/billing`, chỉ SuperAdmin), có hiệu lực ngay — không sửa
`.env`, không khởi động lại.

1. **Tài khoản nhận tiền** — mục *Tài khoản ngân hàng nhận tiền*: điền mã BIN
   ngân hàng, số tài khoản, tên chủ tài khoản, bấm Lưu. Chưa có số tài khoản thì
   trang mua chỉ hiện bảng giá.
2. **Tạo khóa bí mật** — mục *Webhook thanh toán*, bấm **Tạo khóa mới**. Khóa
   (64 ký tự hex) hiện đầy đủ **một lần**: bấm *Chép* và giao cho dịch vụ kiểm tra
   giao dịch qua kênh an toàn (không gửi qua chat/email thường). Sau khi rời
   trang chỉ còn thấy 4 ký tự cuối, đủ để đối chiếu với khóa dịch vụ đang dùng.
3. **Kiểm tra đã bật** — gọi với body rỗng; phải nhận `400`, không còn `503`:

   ```bash
   curl -s -X POST https://<máy-chủ>/api/v1/billing/payments/webhook \
     -H "X-Webhook-Secret: <khóa>" -H 'Content-Type: application/json' -d '{}'
   # {"error":{"message":"source is required …"}}  ← đã bật
   # "the payment webhook is not configured"        ← chưa có khóa, làm lại bước 2
   ```

Tắt webhook: bấm **Tắt webhook** (lưu khóa rỗng; mọi giao dịch đẩy tới bị `503`
cho tới khi tạo khóa mới).

**Khóa nằm ở đâu:** system setting `billing.webhook_secret` (khai báo trong
registry ở `internal/application/service/system_setting.go`, nhóm `billing`,
đánh dấu bí mật). API quản trị chỉ trả giá trị đã che (`••••••••` + 4 ký tự cuối),
nhật ký kiểm toán chỉ ghi "[set]", server đọc giá trị thật ở mỗi request. Biến môi
trường `BILLING_WEBHOOK_SECRET` chỉ là **dự phòng** khi chưa lưu giá trị nào
(như các setting khác có `EnvName`); khóa đã lưu trên trang luôn được ưu tiên.

### Một lần khi triển khai: IP của bên gọi

Giới hạn theo IP (mục 5) cần backend thấy đúng IP người gọi. Đây là cấu hình hạ
tầng, đọc lúc khởi động, nên vẫn nằm trong `.env` của máy chủ:

```dotenv
# chỉ tin X-Forwarded-For từ mạng Docker chứa nginx — chỉ cho docker compose
WEKNORA_TRUSTED_PROXIES=127.0.0.0/8,::1/128,<subnet mạng Docker>
```

1. Lấy `<subnet mạng Docker>` bằng
   `docker network inspect <project>_WeKnora-network --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}'`
   (thường nằm trong `172.16.0.0/12`, nhưng máy chạy nhiều project Docker có thể
   được cấp `192.168.x.0/20`). Triển khai bằng **helm/k8s thì không đặt biến này**
   — giữ mặc định.
2. Tạo lại container: `docker compose up -d --force-recreate app` (và
   `docker compose up -d --build frontend` khi image frontend có bản nginx mới).
3. **Kiểm tra IP đúng** — mở trang web từ một máy trong LAN rồi xem
   `docker compose logs app --tail 50 | grep client_ip`: phải thấy IP của máy đó.
   Nếu mọi request đều mang cùng một IP `172.x`/`192.168.x` (IP của nginx) là
   `WEKNORA_TRUSTED_PROXIES` sai subnet — khi đó mọi người dùng chung một bộ đếm
   giới hạn (đăng nhập 30 lần/phút cho cả hệ thống). Sửa subnet và tạo lại container.

## 2. Thử toàn bộ luồng một lần

Dùng một **tổ chức hoặc tài khoản thử** — giao dịch giả sẽ kích hoạt gói thật.

1. Đăng nhập tài khoản thử, mua một gói, ghi lại **mã đơn** (`WR…`) và **số tiền**.
2. Giả lập dịch vụ kiểm tra:

   ```bash
   curl -s -X POST https://<máy-chủ>/api/v1/billing/payments/webhook \
     -H "X-Webhook-Secret: <khóa>" -H 'Content-Type: application/json' \
     -d '{"source":"test","transaction_id":"TEST-001","amount_vnd":<số tiền>,
          "content":"<mã đơn> chuyen khoan","transacted_at":"2026-10-18T14:05:00+07:00"}'
   ```

3. Kết quả phải có `"status":"matched"` và đơn `"status":"paid"`,
   `"confirmed_by":"payment:test"`. Trang đơn của người mua chuyển sang đã thanh toán.
4. Gửi lại đúng lệnh trên → `"duplicate": true`, gói **không** được gia hạn thêm.

> Từ 19/10/2026 (`docs/agent-addon-plans.md` §8.3): đơn có thể kèm agent chuyên ngành
> (`addons`) hoặc chỉ mua agent cho gói đang dùng (`kind = "addon"`). Webhook xác nhận
> các đơn này như mọi đơn: quyền dùng agent được ghi cùng transaction; đơn mua agent cho
> một thuê bao đã bị thay thế bị từ chối và giao dịch ghi `confirm_failed`.

## 3. Việc hằng ngày của SuperAdmin

Giao dịch không tự xác nhận được nằm trong danh sách (chưa có màn hình riêng,
dùng API với token SuperAdmin):

```
GET /api/v1/system/admin/usage/payments?status=unmatched&page=1&page_size=50
→ { "success": true, "data": [ … ], "total": 128, "page": 1, "page_size": 50 }
```

Mới nhất trước; `page` từ 1; `page_size` mặc định 50, lớn hơn 200 thì lấy 200;
không phải số dương → `400`. Bỏ `status` để xem tất cả.

| `status` | nghĩa | nên làm |
|---|---|---|
| `matched` | đã tự xác nhận đơn; `reason` ghi nếu chuyển dư | không (hoàn phần dư nếu cần) |
| `unmatched` | không thấy mã đơn trong nội dung (người mua ghi sai/thiếu) | tìm đơn theo số tiền/thời điểm, bấm "Xác nhận" ở trang *Thanh toán & khuyến mãi* |
| `underpaid` | đúng đơn nhưng chuyển thiếu (`reason`: "transferred X of Y VND") | liên hệ người mua chuyển thêm, rồi xác nhận tay |
| `order_closed` | đơn đã thanh toán/hủy, hoặc đã hết hạn và tiền chuyển sau hạn | hoàn tiền, hoặc tạo đơn mới cho người mua rồi xác nhận |
| `confirm_failed` | khớp đơn nhưng không áp dụng được (vd. gói hiện có nhiều ghế hơn đơn trả) | đọc `reason`; thường là hủy đơn, người mua đặt lại |
| `ignored` | tiền ra, số tiền ≤ 0, hoặc không phải tài khoản nhận | không |

Mỗi giao dịch giữ nguyên body dịch vụ gửi (`raw`) để đối soát với sao kê.

**Khách báo "đã chuyển mà chưa được kích hoạt":** tìm theo `status=unmatched`
và `status=underpaid` quanh thời điểm khách chuyển; không thấy gì → dịch vụ kiểm
tra chưa đẩy giao dịch đó (xem log của dịch vụ, mục 6).

---

## 4. Hợp đồng API cho dịch vụ kiểm tra giao dịch

```
POST /api/v1/billing/payments/webhook
Content-Type: application/json
X-Webhook-Secret: <khóa bí mật>
```

Thay cho `X-Webhook-Secret` có thể dùng `Authorization: Bearer <secret>` hoặc
`Authorization: Apikey <secret>` (dạng SePay gửi). Không cần tài khoản đăng nhập.
Mỗi request **một** giao dịch; body tối đa 64 KB. Gửi song song bao nhiêu
request cũng được, nhưng gửi lần lượt là đủ (giới hạn 300 request/phút).

```json
{
  "source": "bank-poller",
  "transaction_id": "FT26291123456",
  "amount_vnd": 199000,
  "content": "IBFT WRABCD2345 thanh toan goi",
  "account_no": "0011002233",
  "direction": "in",
  "transacted_at": "2026-10-18T14:05:00+07:00"
}
```

| trường | bắt buộc | ghi chú |
|---|---|---|
| `source` | có | tên dịch vụ đẩy (chữ, số, `-_.`, ≤ 32 ký tự; chữ hoa được đổi thành chữ thường); giữ cố định |
| `transaction_id` | có | mã giao dịch của ngân hàng (≤ 128 ký tự); cùng `source` + `transaction_id` gửi lại thì không xử lý lần hai |
| `amount_vnd` | có | số tiền (đồng) dạng **số nguyên JSON** — `199000.0` hay `"199000"` bị `400`, cần đổi trước khi gửi; thiếu hoặc ≤ 0 thì giao dịch được lưu là `ignored` |
| `content` | | nội dung chuyển khoản nguyên văn; mã đơn được tìm trong đây, kể cả khi ngân hàng viết hoa hay dính liền chữ phía sau |
| `account_no` | | tài khoản nhận; có mà khác tài khoản cấu hình thì `ignored` |
| `direction` | | `in` (mặc định) hoặc `out`; tiền ra được lưu là `ignored` |
| `transacted_at` | nên có | thời điểm ngân hàng ghi nhận (RFC 3339); thiếu thì lấy lúc nhận |

**Nên gửi `transacted_at`:** đơn đã chuyển sang "hết hạn" nhưng tiền được
chuyển **trước** hạn (dịch vụ báo trễ) sẽ được mở lại và xác nhận — chỉ biết
được khi có thời điểm ngân hàng. Đơn còn "chờ thanh toán" thì xác nhận được kể cả
khi đã quá hạn mà hệ thống chưa kịp chuyển nó sang "hết hạn".

Một giao dịch chỉ thanh toán **một** đơn: nội dung có nhiều mã thì đơn còn mở
đầu tiên được dùng.

### Kết quả và gửi lại

```json
{
  "success": true,
  "data": {
    "transaction": { "id": "…", "status": "matched", "reason": "", "order_code": "WRABCD2345", … },
    "order": { "order_code": "WRABCD2345", "status": "paid", "confirmed_by": "payment:bank-poller", … },
    "duplicate": false
  }
}
```

| mã | khi | dịch vụ kiểm tra nên |
|---|---|---|
| `200` | đã ghi nhận (kể cả không khớp đơn — xem `status`) | coi là xong |
| `400` | body sai, thiếu `source`/`transaction_id`, có ký tự NUL | ghi log, báo người vận hành; gửi lại y nguyên vẫn lỗi |
| `401` | sai hoặc thiếu secret | giữ giao dịch lại, báo người vận hành; gửi lại sau khi sửa secret |
| `429` | vượt giới hạn (mục 5) | chờ số giây trong `Retry-After` (tối đa) rồi gửi lại; **không bỏ giao dịch** |
| `500` | lỗi lưu trữ — không có gì được ghi | gửi lại (giãn dần) |
| `503` | webhook chưa bật | giữ giao dịch lại, gửi lại sau |
| lỗi mạng | | gửi lại |

Gửi lại luôn an toàn: lần sau trả kết quả đã lưu với `duplicate: true`. Gợi ý:
dịch vụ kiểm tra lưu hàng đợi các giao dịch chưa nhận `200`, và chỉ xóa khỏi
hàng đợi khi nhận `200`.

---

## 5. Bảo mật và triển khai

- **Khóa bí mật** — 32 byte ngẫu nhiên (tạo trong trình duyệt), so sánh thời
  gian hằng; chỉ chấp nhận 32–256 ký tự chữ/số/`-`/`_`. Chỉ có một khóa tại một
  thời điểm. Đổi khóa theo thứ tự: **dừng dịch vụ kiểm tra** → bấm *Tạo khóa mới*
  → cập nhật khóa cho dịch vụ → chạy lại. Nếu để dịch vụ chạy và gửi khóa cũ quá
  10 lần, IP của nó bị chặn 15 phút.
- **Giới hạn theo IP** (bộ đếm trong bộ nhớ, khởi động lại backend là mất):
  - tối đa **300 request/phút** mỗi IP; vượt → `429`, `Retry-After: 60`;
  - sai secret **10 lần trong 15 phút** → IP đó bị chặn mọi request có gửi secret
    (kể cả secret đúng) đến hết 15 phút: `429`, `Retry-After: 900`. Header
    `Authorization` dạng khác (vd. `Basic …`) cũng tính là sai. Request không gửi
    secret nào (probe giám sát…) chỉ nhận `401`, không bị tính.
- **IP được lấy thế nào** — nginx của frontend đặt `X-Forwarded-For` bằng IP thật
  của bên gọi (ghi đè, không nối thêm), nên người gọi không tự khai IP được khi đi
  qua nginx. Backend chỉ nên tin header này từ nginx:
  - docker compose: đặt `WEKNORA_TRUSTED_PROXIES` là subnet mạng Docker chứa
    nginx (mục 1, phần "IP của bên gọi"). Mặc định khi không đặt là tin **mọi** dải nội bộ
    — máy trong LAN gọi thẳng cổng backend sẽ tự khai được IP. Đặt rỗng thì
    không tin proxy nào: mọi người dùng mang IP của nginx. Helm/k8s: giữ mặc định.
  - Compose mặc định mở cổng backend ra mọi card mạng (`APP_PORT=8080`). Nếu
    không cần gọi thẳng backend, đặt `APP_PORT=127.0.0.1:8080` trong `.env` (hoặc
    chặn bằng firewall) và cho dịch vụ kiểm tra gọi qua tên miền/nginx.
  - Nếu trước container frontend còn một load balancer/TLS proxy, đặt
    `REAL_IP_FROM=<IP hoặc CIDR của LB>` (phân tách bằng dấu phẩy) trong `.env`
    rồi tạo lại container `frontend`; nginx sẽ lấy IP client từ header của LB.
    Không đặt thì mọi người dùng mang IP của LB và dùng chung bộ đếm giới hạn.
- **Giao dịch giả** — ai có khóa đều kích hoạt được gói. Giữ khóa như mật
  khẩu quản trị; nghi lộ thì bấm *Tạo khóa mới* ngay và rà `GET …/payments`
  theo `source`. Mọi lần đổi khóa có trong nhật ký kiểm toán (không kèm giá trị).

## 6. Xử lý sự cố

| hiện tượng | nguyên nhân thường gặp | cách xử lý |
|---|---|---|
| `503 the payment webhook is not configured` | chưa tạo khóa, hoặc đã bấm *Tắt webhook* | tạo khóa ở trang *Thanh toán & khuyến mãi* |
| `401 invalid webhook secret` | khóa sai/thừa khoảng trắng, sai tên header, hoặc khóa đã được tạo lại | so 4 ký tự cuối với trang; header `X-Webhook-Secret` hoặc `Authorization: Bearer …` |
| `429` liên tục, `Retry-After: 900` | đã sai khóa ≥ 10 lần từ IP đó | sửa khóa, chờ 15 phút (hoặc tạo lại container `app`) |
| mọi người bị `429` khi đăng nhập/gọi webhook | backend thấy mọi request cùng một IP (subnet `WEKNORA_TRUSTED_PROXIES` sai, hoặc có LB mà chưa đặt `REAL_IP_FROM`) | xem `client_ip` trong log `app` (mục 1, phần "IP của bên gọi", bước 3) |
| giao dịch `ignored`, "not the receiving account" | `account_no` dịch vụ gửi khác số TK cấu hình | sửa cấu hình hoặc bỏ trường `account_no` |
| nhiều `unmatched` | người mua không ghi mã đơn, hoặc ngân hàng cắt nội dung | xác nhận tay; nhắc người mua chép đúng nội dung từ trang thanh toán |
| đơn hết hạn dù khách chuyển đúng hạn | dịch vụ không gửi `transacted_at` | bổ sung `transacted_at`; với đơn đã lỡ: tạo đơn mới và xác nhận tay |

## 7. Tham chiếu kỹ thuật

| thành phần | vị trí |
|---|---|
| bảng giao dịch | `usage_payment_transactions` (migration `000161`), khóa duy nhất `(source, external_id)` |
| xử lý khớp đơn | `internal/application/service/usage_billing_payment.go` |
| handler webhook + danh sách | `internal/handler/usage_billing.go` (`PaymentWebhook`, `AdminListPayments`) |
| route | `internal/router/routes_billing.go` |
| xác thực + giới hạn IP | `internal/middleware/payment_webhook_ratelimit.go` (`PaymentWebhookAuth`) |
| IP client qua nginx | `frontend-next/docker/nginx*.conf`, `REAL_IP_FROM` trong `frontend-next/docker/docker-entrypoint.sh` |
| khóa bí mật | system setting `billing.webhook_secret` (`internal/application/service/system_setting.go`, che ở `maskSecretSetting`); giao diện `frontend-next/components/settings/pages/admin-billing.tsx` |
| biến môi trường | `BILLING_WEBHOOK_SECRET` (dự phòng của setting), `WEKNORA_TRUSTED_PROXIES` (app), `REAL_IP_FROM` (frontend), `APP_PORT` (`.env.example`, `docker-compose.yml`) |
