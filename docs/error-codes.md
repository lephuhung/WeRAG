# Quy ước mã lỗi thống nhất giữa backend và frontend

Tài liệu này là nguồn tham chiếu duy nhất cho mã lỗi của WeRAG. Backend (Go, `internal/errors`)
định nghĩa mã; frontend-next và Word add-in hiển thị theo đúng quy ước ở đây. Mọi thông báo lỗi
trả về cho người dùng viết bằng **tiếng Việt có dấu**; log nội bộ viết tiếng Anh.

## 1. Phong bì lỗi (error envelope)

Mọi lỗi HTTP đi qua `middleware.ErrorHandler` có cùng hình dạng:

```json
{
  "success": false,
  "error": {
    "code": 1003,
    "message": "không tìm thấy kho tri thức",
    "details": { "reason": "…" }
  }
}
```

| Trường | Ý nghĩa |
|---|---|
| `code` | Mã lỗi số nguyên, ổn định, dùng để phân nhánh ở client. Không dịch, không đổi. |
| `message` | Câu tiếng Việt ngắn, hiển thị thẳng cho người dùng được. Không có dấu chấm cuối. |
| `details` | Tùy chọn. Dữ liệu máy đọc: `reason`, danh sách id, giới hạn… Không chứa văn bản để hiển thị. |

Mã HTTP đi kèm do `AppError.HTTPCode` quyết định (xem bảng). Lỗi không phải `AppError` bị gói thành
`500 / 1007 / "Internal server error"`; đây là lỗi lập trình, handler phải luôn trả `AppError`.

Một số endpoint cũ trả `{"success":false,"message":"…"}` không có `error`. Client vẫn đọc được
(`envelopeMessage` thử `error.message` rồi mới tới `message`), nhưng code mới không dùng dạng này.

## 2. Bảng mã lỗi

### 2.1 Lỗi chung (1000–1010)

| Code | HTTP | Hàm tạo (Go) | Thông báo mặc định | Khi nào |
|---|---|---|---|---|
| 1000 | 400 | `NewBadRequestError(msg)` | theo ngữ cảnh | Tham số sai, body không hợp lệ, trạng thái không cho phép thao tác |
| 1001 | 401 | `NewUnauthorizedError(msg)` | theo ngữ cảnh | Chưa đăng nhập, token hết hạn hoặc bị thu hồi |
| 1002 | 403 | `NewForbiddenError(msg)` | theo ngữ cảnh | Đã đăng nhập nhưng không có quyền (RBAC, khác tổ chức, không phải chủ sở hữu) |
| 1003 | 404 | `NewNotFoundError(msg)` | theo ngữ cảnh | Tài nguyên không tồn tại hoặc không thuộc phạm vi người gọi |
| 1004 | 405 | — | | Dự phòng |
| 1005 | 409 | `NewConflictError(msg)` | theo ngữ cảnh | Trùng tên, phiên bản thay đổi, **lượt trả lời trước còn đang chạy** (chat) |
| 1006 | 429 | `NewTooManyRequestsError(msg)` | `quá nhiều yêu cầu, vui lòng thử lại sau` | Rate limit; gói sử dụng từ chối lượt chat (xem 2.5) |
| 1007 | 500 | `NewInternalServerError(msg)` | `lỗi hệ thống` | Lỗi không lường trước. Không lộ chi tiết nội bộ trong `message` |
| 1008 | 503 | `NewServiceUnavailableError(msg)` | `dịch vụ tạm thời không khả dụng` | Mô hình, DocReader, kho vector, Neo4j… không phản hồi |
| 1009 | 504 | — | | Hết thời gian chờ dịch vụ phụ thuộc |
| 1010 | 400 | `NewValidationError(msg)` | theo ngữ cảnh | Dữ liệu đúng cú pháp nhưng sai ràng buộc nghiệp vụ |

### 2.2 Tổ chức (2000–2005)

| Code | HTTP | Hàm tạo | Thông báo |
|---|---|---|---|
| 2000 | 404 | `NewTenantNotFoundError()` | không tìm thấy tổ chức |
| 2001 | 409 | `NewTenantAlreadyExistsError()` | tổ chức đã tồn tại |
| 2002 | 403 | `NewTenantInactiveError()` | tổ chức đã bị vô hiệu hóa |
| 2003 | 400 | — | tên tổ chức là bắt buộc |
| 2004 | 400 | — | trạng thái tổ chức không hợp lệ |
| 2005 | 403 | `NewTenantCreationDisabledError()` | đã tắt tự tạo tổ chức; hãy tham gia tổ chức qua lời mời |

### 2.3 Cấu hình trợ lý (2100–2103)

| Code | HTTP | Hàm tạo | Thông báo |
|---|---|---|---|
| 2100 | 400 | `NewAgentMissingThinkingModelError()` | cần chọn mô hình suy luận trước khi bật chế độ trợ lý |
| 2101 | 400 | `NewAgentMissingAllowedToolsError()` | cần chọn ít nhất một công cụ được phép |
| 2102 | 400 | `NewAgentInvalidMaxIterationsError()` | số vòng lặp tối đa phải là số nguyên dương, hoặc -1 nếu không giới hạn |
| 2103 | 400 | `NewAgentInvalidTemperatureError()` | tham số temperature phải nằm trong khoảng 0-2 |

### 2.4 Kho vector và mô hình (2200–2300)

| Code | HTTP | Hàm tạo | Thông báo mặc định |
|---|---|---|---|
| 2200 | 400 | `NewVectorStoreBindingInvalidError(msg)` | không tìm thấy kho vector |
| 2201 | 503 | `NewVectorStoreUnavailableError(msg)` | kho vector tạm thời không khả dụng |
| 2300 | 400 | `NewModelInUseError(msg, details)` | theo ngữ cảnh; `details` liệt kê nơi đang dùng mô hình |

### 2.5 Gói sử dụng từ chối lượt chat (`details.reason`, HTTP 429, code 1006)

| `reason` | Ý nghĩa | Khóa i18n frontend |
|---|---|---|
| `no_subscription` | Tổ chức hoặc tài khoản chưa có gói | `chat.err.usage.noSubscription` |
| `expired` | Gói đã hết hạn | `chat.err.usage.expired` |
| `no_seat` | Tài khoản chưa được cấp chỗ trong gói của tổ chức | `chat.err.usage.noSeat` |
| `limit_exhausted` | Hết hạn mức của kỳ | `chat.err.usage.limit` |
| `session_limit` | Cuộc trò chuyện chạm trần của gói; client mời mở phiên mới kèm tóm tắt (`POST /sessions/:id/continue`) | `chat.err.usage.sessionLimit` |

Hằng số Go: `types.UsageDenial*` trong `internal/types/usage_plan.go`. Frontend: `USAGE_REASON_KEYS`
trong `frontend-next/lib/api/chat-errors.ts` (Word add-in dùng bản sao qua `npm run sync-shared`).

### 2.6 Mã lỗi phân tích tài liệu (chuỗi, không phải HTTP)

Đây là `knowledge.error_code` của một tài liệu tải lên, do pipeline phân tích ghi lại; API trả
200 với trường `error_code` + `error_message`. Định nghĩa tại `internal/errors/parse_error_codes.go`.

| Mã | Ý nghĩa | Gợi ý cho người dùng |
|---|---|---|
| `DOCREADER_TIMEOUT` | DocReader quá thời gian | Tệp quá lớn hoặc dịch vụ bận; thử lại |
| `DOCREADER_UNAVAILABLE` | Không có DocReader cho loại tệp | Liên hệ quản trị |
| `DOCREADER_PARSE_FAILED` | DocReader báo lỗi phân tích | Tệp hỏng hoặc định dạng lạ |
| `PARSE_NO_TEXT` | Không trích được chữ | Tệp scan không chữ; cần OCR hoặc tệp gốc |
| `CHUNKING_FAILED` | Lỗi khi tách đoạn | Hiếm; báo quản trị |
| `EMBEDDING_RATE_LIMIT` | Nhà cung cấp embedding trả 429 | Hệ thống tự thử lại |
| `EMBEDDING_PROVIDER_FAIL` | Lỗi embedding khác | Kiểm tra cấu hình mô hình |
| `VECTORSTORE_WRITE_FAILED` | Ghi kho vector thất bại | Kiểm tra kho vector |
| `MULTIMODAL_VLM_FAILED` | Một ảnh OCR/caption lỗi | Tài liệu vẫn dùng được, ảnh đó thiếu mô tả |
| `MULTIMODAL_ALL_FAILED` | Mọi ảnh đều lỗi | Kiểm tra dịch vụ OCR |

## 3. Quy tắc phía backend

1. Handler chỉ trả `AppError` (qua `c.Error(...)`), không tự `c.JSON` thông báo lỗi.
2. `message` là câu tiếng Việt hoàn chỉnh cho người dùng; không nhét `err.Error()` của thư viện vào `message`,
   chi tiết kỹ thuật ghi log hoặc đưa vào `details` khi client cần.
3. Chọn mã theo ý nghĩa, không theo tầng phát sinh: thiếu quyền là 1002 dù phát hiện ở repository.
4. Lỗi nghiệp vụ lặp lại ở nhiều nơi (ví dụ "lượt trước còn đang chạy") đáng có mã riêng trong dải 2xxx
   để client phân nhánh mà không phải so khớp chuỗi. Khi thêm mã mới: thêm hằng vào `internal/errors/errors.go`,
   hàm tạo kèm thông báo mặc định tiếng Việt, cập nhật bảng ở đây và khóa i18n ở frontend (mục 4).
5. Thuật ngữ thống nhất: kho tri thức, tổ chức, cuộc trò chuyện, trợ lý, mô hình, đoạn (chunk), thẻ, tài liệu,
   người dùng, thành viên, gói sử dụng, bộ nhớ, kho vector.
6. Không còn thông báo tiếng Trung trong `internal/`; `rg -P '\p{Han}' internal --glob '!*_test.go'` phải rỗng
   trừ dữ liệu xử lý văn bản (dấu câu, bảng chuyển đổi trong `internal/textconv`).

## 4. Quy tắc phía frontend-next và Word add-in

1. `ApiError` (`lib/api-client.ts`) giữ `status`, `message` (từ `error.message`) và `payload` (cả phong bì).
   `payload.error.code` và `payload.error.details.reason` là thứ để phân nhánh; `message` chỉ để hiển thị.
2. Thứ tự hiển thị: (a) nếu `code`/`reason` có khóa i18n riêng → dùng khóa; (b) nếu backend đưa `message` cho lỗi 4xx
   → hiển thị nguyên văn (đã là tiếng Việt); (c) còn lại dùng khóa chung theo HTTP: 401 `chat.err.unauthorized`,
   403 `chat.err.forbidden`, 404 `chat.err.notFound`, 413 `chat.err.tooLarge`, 429 `chat.err.tooMany`,
   502–504 `chat.err.unavailable`, 5xx `chat.err.server`, lỗi mạng `chat.err.network`, khác `chat.err.generic`.
   Hàm chuẩn: `chatErrorKey()` trong `lib/api/chat-errors.ts`; màn hình ngoài chat dùng cùng ánh xạ.
3. Mã có khóa riêng hiện tại: `1005` (`ERR_TURN_RUNNING`) → `chat.err.turnRunning`; các `reason` ở mục 2.5.
4. Không hard-code câu tiếng Anh trong component; chuỗi dự phòng của `api-client.ts` ("Request failed",
   "Please sign in again") chỉ dùng khi backend không trả phong bì và phải được bọc bằng khóa i18n khi hiển thị.
5. Word add-in dùng chung `chat-errors.ts` và các khóa `chat.err.*` qua `scripts/shared-files.json`; sửa ở
   frontend-next rồi `npm run sync-shared`, không sửa tay bản sao.

## 5. Checklist khi thêm hoặc đổi một lỗi

- [ ] Hằng số và hàm tạo trong `internal/errors/errors.go`, thông báo mặc định tiếng Việt
- [ ] Bảng mục 2 của tài liệu này
- [ ] Khóa i18n `en` + `vi` trong `frontend-next/lib/i18n.tsx` nếu client cần câu riêng
- [ ] Ánh xạ trong `chat-errors.ts` (theo `code` hoặc `reason`), rồi `npm run sync-shared` cho add-in
- [ ] Test handler kiểm tra đúng `code` và HTTP, không kiểm tra nguyên văn `message`
