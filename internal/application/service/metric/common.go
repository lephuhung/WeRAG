package metric

import (
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

func sum(m map[string]int) int {
	s := 0
	for _, v := range m {
		s += v
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func splitSentences(text string) []string {
	// Biên dịch regex (khớp dấu chấm câu CJK hoặc dấu chấm ASCII)
	re := regexp.MustCompile(`([。.])`)

	// Tách văn bản và giữ lại dấu phân cách để định vị
	split := re.Split(text, -1)

	var sentences []string
	current := strings.Builder{}

	for i, s := range split {
		// Luân phiên lấy đoạn văn bản và dấu phân cách (chỉ số lẻ là dấu phân cách)
		if i%2 == 0 {
			current.WriteString(s)
		} else {
			// Gặp dấu phân cách thì kết thúc câu hiện tại
			if current.Len() > 0 {
				sentence := strings.TrimSpace(current.String())
				if sentence != "" {
					sentences = append(sentences, sentence)
				}
				current.Reset()
			}
		}
	}

	// Xử lý đoạn cuối không có dấu phân cách
	if remaining := strings.TrimSpace(current.String()); remaining != "" {
		sentences = append(sentences, remaining)
	}

	return sentences
}

func splitIntoWords(sentences []string) []string {
	// Regex khớp các khối CJK, khối Latin và ký tự khác
	re := regexp.MustCompile(`([\p{Han}]+)|([a-zA-Z0-9_.,!?]+)|(\p{P})`)

	var tokens []string
	for _, text := range sentences {
		matches := re.FindAllStringSubmatch(text, -1)

		for _, groups := range matches {
			chineseBlock := groups[1]
			englishBlock := groups[2]
			punctuation := groups[3]

			switch {
			case chineseBlock != "": // xử lý phần CJK
				words := types.Jieba.Cut(chineseBlock, true)
				tokens = append(tokens, words...)
			case englishBlock != "": // xử lý phần Latin
				engTokens := strings.Fields(englishBlock)
				tokens = append(tokens, engTokens...)
			case punctuation != "": // giữ lại dấu câu
				tokens = append(tokens, punctuation)
			}
		}
	}
	return tokens
}

func ToSet[T comparable](li []T) map[T]struct{} {
	res := make(map[T]struct{}, len(li))
	for _, v := range li {
		res[v] = struct{}{}
	}
	return res
}

func SliceMap[T any, Y any](li []T, fn func(T) Y) []Y {
	res := make([]Y, len(li))
	for i, v := range li {
		res[i] = fn(v)
	}
	return res
}

func Hit[T comparable](li []T, set map[T]struct{}) int {
	count := 0
	for _, v := range li {
		if _, exist := set[v]; exist {
			count++
		}
	}
	return count
}

func Fold[T any, Y any](slice []T, initial Y, f func(Y, T) Y) Y {
	accumulator := initial
	for _, item := range slice {
		accumulator = f(accumulator, item)
	}
	return accumulator
}
