# Skills 示例

本目录包含 Agent Skills 功能的示例。

## 目录结构

```
skills/
├── README.md              # 本文件
├── legal-document-summary/SKILL.md
├── legal-document-comparison/SKILL.md
├── legal-latest-guidance/SKILL.md
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
| [`legal-document-summary`](legal-document-summary/SKILL.md) | Tóm tắt phạm vi, nội dung và mốc thời gian được nêu trong văn bản. |
| [`legal-document-comparison`](legal-document-comparison/SKILL.md) | Đối chiếu các văn bản/điều khoản và căn cứ về điểm khác nhau. |
| [`legal-latest-guidance`](legal-latest-guidance/SKILL.md) | Trả lời cùng một vấn đề theo hướng dẫn mới hơn trong các tài liệu nội bộ có thể đọc. |

Đây là **các skill mẫu, không tự cài** khi checkout repo. Để dùng cho agent trong ứng dụng:

1. Đảm bảo agent có quyền truy vấn các văn bản pháp luật đã nạp vào kho tri thức trong WeKnora; skill không tải thêm văn bản.
2. Đóng gói **riêng từng thư mục skill** (giữ `SKILL.md` tại gốc của gói), rồi vào phần thiết lập **sandbox/Skill** của không gian làm việc để tải zip lên cấu hình sandbox được chọn. Có thể dùng URL công khai trỏ đến `SKILL.md` nếu môi trường cho phép; với repo riêng tư, dùng zip. Xem [hướng dẫn Agent Skills](../../docs/agent-skills.md#安装租户技能) để biết nguồn cài được hỗ trợ.
3. Gắn cấu hình sandbox đó cho agent ở chế độ **smart-reasoning**, bật Skills và chọn các skill cần dùng; nếu cấu hình agent có danh sách cho phép, thêm đúng tên skill. Agent đọc nội dung khi cần qua `read_file(path="skill://legal-document-summary/SKILL.md")` (thay tên cho hai skill còn lại). Ba skill này không có script và không cần `shell_exec`.

**Giới hạn:** Các skill chỉ hướng dẫn cách dùng văn bản agent **đọc được trong hệ thống**, không tra cứu web và không tự xác nhận văn bản “mới nhất” ngoài tập tài liệu đã đối chiếu. Cùng vấn đề và cùng phạm vi thì ưu tiên hướng dẫn có hiệu lực mới hơn ở thời điểm hỏi; không vì ngày mới hơn mà suy ra văn bản cũ hết hiệu lực. Thiếu ngày hiệu lực, phạm vi hoặc căn cứ quan hệ văn bản thì nêu giới hạn thay vì đoán.

## 示例：pdf-processing

这是一个功能完整的示例技能，展示了：

- **SKILL.md**: 包含 YAML frontmatter 的主文件
- **FORMS.md**: 补充参考文档
- **scripts/**: 可在沙箱中执行的 Python 脚本

### 技能描述

```yaml
name: pdf-processing
description: Extract text and tables from PDF files, fill forms, merge documents.
```

### 包含的脚本

| 脚本 | 功能 |
|------|------|
| `analyze_form.py` | 分析 PDF 表单字段 |
| `extract_text.py` | 从 PDF 提取文本 |

### 使用示例

Agent 会根据用户请求自动调用：

```
用户: "分析一下这个 PDF 表单有哪些字段"

Agent: 
  1. 识别匹配 pdf-processing 技能
  2. 调用 read_file(path="skill://pdf-processing/SKILL.md") 加载技能内容
  3. 调用 shell_exec(skill_name="pdf-processing", command=...) 执行 analyze_form.py
  4. 返回表单字段分析结果
```
