# Skills 示例

本目录包含 Agent Skills 功能的示例。

## 目录结构

```
skills/
├── README.md              # 本文件
├── legal-document-summary/SKILL.md
└── pdf-processing/        # PDF 处理技能示例
    ├── SKILL.md           # 主文件（Level 2）
    ├── FORMS.md           # 补充文档（Level 3）
    └── scripts/           # 可执行脚本
        ├── analyze_form.py
        └── extract_text.py
```

## 快速开始

### 运行 Demo

```bash
go run ./cmd/skills-demo/main.go
```

### 创建新 Skill

1. 在本目录创建新文件夹：

```bash
mkdir my-new-skill
```

2. 创建 `SKILL.md`：

```markdown
---
name: my-new-skill
description: Description of what this skill does and when to use it.
---

# My New Skill

Instructions for the agent...
```

3. 添加脚本（可选）：

```bash
mkdir my-new-skill/scripts
# 添加你的脚本
```

## 详细文档

完整文档请参阅：[Agent Skills 文档](../../docs/agent-skills.md)

## Ví dụ: Agent Skills cho văn bản pháp luật Việt Nam

| Skill | Khi dùng |
|------|----------|
| [`legal-document-summary`](legal-document-summary/SKILL.md) | Tóm tắt phạm vi, nội dung và mốc thời gian được nêu trong văn bản; đối chiếu hai hay nhiều văn bản/điều khoản. |

Skill trên được **đóng gói sẵn khi build ứng dụng** và có mặt cho các agent ở chế độ **Smart Reasoning**, trừ Soạn thảo văn bản, Phân tích dữ liệu và các agent Wiki (chúng có tool riêng cho việc này); không cần sandbox, tải zip hay bật lựa chọn Skills tenant. Skill chỉ chứa `SKILL.md`, không chạy script. Agent thấy tên và mô tả trước, rồi đọc hướng dẫn khi cần bằng `read_file(path="skill://legal-document-summary/SKILL.md")`. `shell_exec(skill_name=...)` không chạy được skill tích hợp, kể cả khi tenant đã cài một gói trùng tên. `none` và `selected` chỉ giới hạn các skill tenant. Quick Answer/RAG không dùng skill tích hợp.

Ba skill trước đây (`legal-document-comparison`, `legal-latest-guidance`, `legal-question-abbreviations`) đã bỏ ngày 2026-10-18: hiệu lực văn bản do `check_legal_status` và khối "Lịch sử sửa đổi" do code sinh ra đảm nhận, từ viết tắt do cổng phân giải trong backend đảm nhận; phần đối chiếu còn hữu ích đã gộp vào `legal-document-summary`.

Các file Markdown tại đây là **nguồn nội dung duy nhất**: sửa trong Git cần build và triển khai lại ứng dụng. Việc cài zip trong [hướng dẫn Agent Skills](../../docs/agent-skills.md#安装租户技能) vẫn áp dụng cho **skill tùy biến khác**, không cần cho skill tích hợp. Giao diện catalog/picker có thể chỉ hiển thị skill tenant, không quyết định việc bật skill tích hợp. Agent vẫn phải có quyền truy vấn văn bản pháp luật đã nạp vào kho tri thức WeKnora; skill không tải thêm văn bản.

**Giới hạn:** Skill chỉ hướng dẫn cách dùng văn bản agent **đọc được trong hệ thống**, không tra cứu web. Tình trạng hiệu lực lấy từ `check_legal_status` khi có công cụ này; không vì ngày mới hơn mà suy ra văn bản cũ hết hiệu lực. Thiếu căn cứ thì nêu giới hạn thay vì đoán.
