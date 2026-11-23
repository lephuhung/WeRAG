package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// the Word add-in answer of 23/11/2026: the document said 500.000.000 VNĐ
const moneyAnswer = "**1. Xe ô tô thứ nhất**\n*   **Biển số:** 38A-29600\n" +
	"*   **Giá trị:** 500.000.000 VNĐ (05 tỷ đồng)\n*   **Tình trạng:** Đang sử dụng\n\n" +
	"**2. Xe ô tô thứ hai**\n*   **Biển số:** 38A-56218\n" +
	"*   **Giá trị:** 400.000.000 VNĐ (04 tỷ đồng)\n*   **Tình trạng:** Đang sử dụng"

const moneyAnswerFixed = "**1. Xe ô tô thứ nhất**\n*   **Biển số:** 38A-29600\n" +
	"*   **Giá trị:** 500.000.000 VNĐ (500 triệu đồng)\n*   **Tình trạng:** Đang sử dụng\n\n" +
	"**2. Xe ô tô thứ hai**\n*   **Biển số:** 38A-56218\n" +
	"*   **Giá trị:** 400.000.000 VNĐ (400 triệu đồng)\n*   **Tình trạng:** Đang sử dụng"

func TestFixMoneyReadings(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, moneyAnswerFixed, fixMoneyReadings(ctx, moneyAnswer))
	for in, want := range map[string]string{
		// right readings stay
		"500.000.000 VNĐ (500 triệu đồng)":  "500.000.000 VNĐ (500 triệu đồng)",
		"1.500.000.000 đồng (1,5 tỷ đồng)":  "1.500.000.000 đồng (1,5 tỷ đồng)",
		"5 tỷ đồng (5.000.000.000 VNĐ)":     "5 tỷ đồng (5.000.000.000 VNĐ)",
		"2.500 tỷ (2.500.000.000.000 đồng)": "2.500 tỷ (2.500.000.000.000 đồng)",
		"1,5 tỷ đồng (1.500 triệu đồng)":    "1,5 tỷ đồng (1.500 triệu đồng)",
		// wrong readings get the amount's value
		"1.500.000.000 đồng (15 tỷ đồng)":       "1.500.000.000 đồng (1,5 tỷ đồng)",
		"2.000.000 VNĐ (2 tỷ)":                  "2.000.000 VNĐ (2 triệu)",
		"Giá 750,000,000 VND (khoảng 7,5 tỷ).":  "Giá 750,000,000 VND (750 triệu).",
		"5 tỷ đồng (500.000.000 VNĐ)":           "5 tỷ đồng (5.000.000.000 VNĐ)",
		"1.234.567 đồng (12 triệu đồng) là lỗi": "1.234.567 đồng là lỗi",
		// not a reading: untouched
		"Điều 5 (05 tỷ đồng)":            "Điều 5 (05 tỷ đồng)",
		"02 xe ô tô (03 chiếc)":          "02 xe ô tô (03 chiếc)",
		"500.000.000 VNĐ (đã nộp)":       "500.000.000 VNĐ (đã nộp)",
		"500.000.000 VNĐ (400.000.000)":  "500.000.000 VNĐ (400.000.000)",
		"ngày 15/11/2018 (05 tỷ đồng)":   "ngày 15/11/2018 (05 tỷ đồng)",
		"Không có số tiền (chỉ có chữ).": "Không có số tiền (chỉ có chữ).",
	} {
		assert.Equal(t, want, fixMoneyReadings(ctx, in), in)
	}
}

// Every way of cutting the answer streams the fixed answer, and the wrong
// reading never reaches the client, even in part.
func TestMoneyGuardStreamsLikeTheFixedAnswer(t *testing.T) {
	ctx := context.Background()
	for _, size := range []int{1, 2, 3, 5, 7, 11, 64, 1000} {
		var out strings.Builder
		g := newMoneyGuard(ctx, func(s string) {
			assert.NotContains(t, s, "tỷ", "size %d", size)
			out.WriteString(s)
		})
		runes := []rune(moneyAnswer)
		for i := 0; i < len(runes); i += size {
			end := min(i+size, len(runes))
			g.Write(string(runes[i:end]))
		}
		g.Flush()
		assert.Equal(t, moneyAnswerFixed, out.String(), "size %d", size)
	}
	// byte-wise cuts split runes: still the same text
	var out strings.Builder
	g := newMoneyGuard(ctx, func(s string) { out.WriteString(s) })
	for i := 0; i < len(moneyAnswer); i++ {
		g.Write(moneyAnswer[i : i+1])
	}
	g.Flush()
	assert.Equal(t, moneyAnswerFixed, out.String())
}
