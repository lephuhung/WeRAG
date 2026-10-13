package tools

import (
	"regexp"
	"strconv"
)

// The user who asks for "2 phương án" must get two versions on the proposal
// card (the card's buttons are the only way to pick one). The model wrote
// both in the chat and sent one to the tool, so the tool reads the request:
// it turns options on and refuses a call with fewer versions than asked.

var versionCountRe = regexp.MustCompile(`\b(\d+|hai|ba|bon|nhieu|vai|mot vai|mot so|cac|nhung)\s+(phuong an|cach viet|phien ban|lua chon|cach dien dat|goi y|ban)\b`)

// requestedVersionCount is how many versions the request asks for, capped at
// maxRewriteVariants; 0 when it asks for none in particular.
func requestedVersionCount(query string) int {
	m := versionCountRe.FindStringSubmatch(foldSearch(query))
	if m == nil {
		return 0
	}
	n := 0
	switch m[1] {
	case "hai", "nhieu", "vai", "mot vai", "mot so", "cac", "nhung":
		n = 2
	case "ba":
		n = 3
	case "bon":
		n = 4
	default:
		n, _ = strconv.Atoi(m[1])
	}
	if n < 2 {
		return 0
	}
	if n > maxRewriteVariants {
		n = maxRewriteVariants
	}
	return n
}

// errFewerVersions asks the model to call again with every version asked for.
func errFewerVersions(asked, got int) string {
	return "Người dùng yêu cầu " + strconv.Itoa(asked) + " phương án nhưng lời gọi chỉ có " + strconv.Itoa(got) +
		": gọi lại với options_requested=true, edits[0].new là phương án 1 và variants gồm " + strconv.Itoa(asked-1) +
		" bản còn lại, khác nhau về cách diễn đạt. Đừng liệt kê phương án trong câu trả lời: chỉ thẻ đề xuất mới có nút chọn."
}
