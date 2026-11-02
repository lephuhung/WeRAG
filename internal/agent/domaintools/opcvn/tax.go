// Package opcvn holds the fixed-formula calculations of the
// opc-vn-business-law domain pack (docs/domain-pack-standard.md §4.1).
//
// Everything here is a pure function: no I/O, no network, no database, no
// files, and no default for any legal parameter (tax rate, revenue ratio).
// The caller — the agent tool in internal/agent/tools — only parses the
// model's arguments and renders the result.
package opcvn

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Upper bound on amounts: far above any small business, low enough that a
// misplaced exponent is refused instead of printed as a 25-digit number.
const maxAmount = 1e15

// Recommendations, word for word from scripts/so_sanh_thue.py of the source
// skill (Diginno, MIT).
const (
	RecommendEquivalent = "Hai cách gần như tương đương về thuế TNDN; chọn theo mức độ đơn giản và khả năng khấu trừ GTGT."
	RecommendDeclared   = "Kê khai trên lợi nhuận có lợi hơn về thuế TNDN (nhớ kiểm tra GTGT đầu vào)."
	RecommendRevenue    = "Cách tính theo % doanh thu có lợi hơn về thuế TNDN (nhưng thường mất khấu trừ GTGT đầu vào)."
)

// NotIncluded is what the comparison leaves out; it is part of every
// assumptions block.
const NotIncluded = "Chưa tính GTGT, lệ phí môn bài, ưu đãi, chi phí không được trừ."

// TaxInput are the four figures of the comparison. All four are required:
// unlike the source script, there is no default tax rate or ratio.
type TaxInput struct {
	// Revenue is the annual revenue in đồng (doanh_thu).
	Revenue float64
	// Profit is the profit before tax in đồng (loi_nhuan); 0 for a loss.
	Profit float64
	// RevenueRatio is the tax ratio on revenue in PERCENT (ty_le_doanh_thu):
	// 1.5 means 1.5 %, 0.5 means 0.5 %.
	RevenueRatio float64
	// TaxRate is the corporate income tax rate as a fraction (thue_suat),
	// e.g. 0.2 for 20 %.
	TaxRate float64
}

// TaxComparison is the result, field for field the script's KetQua.
type TaxComparison struct {
	Revenue        float64 // doanh_thu
	Profit         float64 // loi_nhuan
	RevenueRatio   float64 // ty_le_doanh_thu, normalised to a fraction
	TaxRate        float64 // thue_suat
	TaxOnRevenue   float64 // thue_theo_doanh_thu = doanh_thu × tỷ lệ
	TaxOnProfit    float64 // thue_theo_loi_nhuan = lợi nhuận × thuế suất
	Margin         float64 // bien_loi_nhuan = lợi nhuận / doanh thu
	BreakEven      float64 // bien_hoa_von = tỷ lệ / thuế suất
	Difference     float64 // chenh_lech = thuế kê khai − thuế theo % doanh thu
	Recommendation string  // khuyen_nghi
	Assumptions    []string
}

// NormalizeRatio turns the revenue ratio, given in percent (0 < x < 100),
// into a fraction. The source script also took fractions (0.015 = 1.5 %),
// which made 0.5 mean 50 % there; a percent-only input has one reading.
func NormalizeRatio(percent float64) (float64, error) {
	if !finite(percent) || percent <= 0 {
		return 0, errors.New("Tỷ lệ phải lớn hơn 0")
	}
	if percent >= 100 {
		return 0, errors.New("Tỷ lệ không hợp lệ: ghi theo phần trăm, phải < 100 (ví dụ 1.5 cho 1,5%)")
	}
	return percent / 100, nil
}

// CompareTaxMethods compares corporate income tax paid as a ratio of
// revenue with tax declared on profit (port of tinh() in so_sanh_thue.py).
func CompareTaxMethods(in TaxInput) (*TaxComparison, error) {
	switch {
	case !finite(in.Revenue) || in.Revenue <= 0:
		return nil, errors.New("Doanh thu phải lớn hơn 0")
	case !finite(in.Profit) || in.Profit < 0:
		return nil, errors.New("Lợi nhuận không được âm (nhập 0 nếu lỗ)")
	case in.Profit > in.Revenue:
		return nil, errors.New("Lợi nhuận không thể lớn hơn doanh thu")
	case in.Revenue > maxAmount:
		return nil, fmt.Errorf("Doanh thu vượt giới hạn %s đ; kiểm tra lại đơn vị (đồng)", FormatVND(maxAmount))
	}
	ratio, err := NormalizeRatio(in.RevenueRatio)
	if err != nil {
		return nil, err
	}
	if err := checkTaxRate(in.TaxRate); err != nil {
		return nil, err
	}

	out := &TaxComparison{
		Revenue:      in.Revenue,
		Profit:       in.Profit,
		RevenueRatio: ratio,
		TaxRate:      in.TaxRate,
		TaxOnRevenue: in.Revenue * ratio,
		TaxOnProfit:  in.Profit * in.TaxRate,
		Margin:       in.Profit / in.Revenue,
		BreakEven:    ratio / in.TaxRate,
	}
	out.Difference = out.TaxOnProfit - out.TaxOnRevenue
	switch {
	case math.Abs(out.Difference) < 1:
		out.Recommendation = RecommendEquivalent
	case out.Difference < 0:
		out.Recommendation = RecommendDeclared
	default:
		out.Recommendation = RecommendRevenue
	}
	out.Assumptions = []string{
		"Doanh thu năm " + FormatVND(in.Revenue) + " và lợi nhuận trước thuế " + FormatVND(in.Profit) +
			" là số người hỏi cung cấp, chưa được kiểm tra.",
		"Lợi nhuận trước thuế được coi là thu nhập chịu thuế, không điều chỉnh chi phí không được trừ hay thu nhập khác.",
		"Tỷ lệ % doanh thu " + FormatPercent(ratio, 2) + " và thuế suất TNDN " + FormatPercent(in.TaxRate, -1) +
			" là tham số đầu vào; phải đối chiếu văn bản thuế hiện hành tại thời điểm kê khai.",
		"Chỉ so sánh thuế thu nhập doanh nghiệp. " + NotIncluded,
	}
	return out, nil
}

// Render is the report the model reads: figures, recommendation, then the
// assumptions block. The disclaimer is not here: the pack's footer is
// appended by code to the answer.
func (c *TaxComparison) Render() string {
	var b strings.Builder
	b.WriteString("So sánh thuế TNDN: tính theo % doanh thu và kê khai trên lợi nhuận\n")
	rows := [][2]string{
		{"Doanh thu", FormatVND(c.Revenue)},
		{"Lợi nhuận trước thuế", FormatVND(c.Profit)},
		{"Biên lợi nhuận", FormatPercent(c.Margin, 2)},
		{"Tỷ lệ % doanh thu", FormatPercent(c.RevenueRatio, 2)},
		{"Thuế suất TNDN", FormatPercent(c.TaxRate, -1)},
		{"Thuế theo % doanh thu", FormatVND(c.TaxOnRevenue)},
		{"Thuế theo lợi nhuận (kê khai)", FormatVND(c.TaxOnProfit)},
		{"Chênh lệch (kê khai − % doanh thu)", FormatVND(c.Difference)},
		{"Biên lợi nhuận hòa vốn", FormatPercent(c.BreakEven, 2)},
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "- %s: %s\n", row[0], row[1])
	}
	fmt.Fprintf(&b, "\nĐọc kết quả: %s\n", c.Reading())
	fmt.Fprintf(&b, "Khuyến nghị sơ bộ: %s\n", c.Recommendation)
	b.WriteString("\nGiả định:\n")
	for _, a := range c.Assumptions {
		fmt.Fprintf(&b, "- %s\n", a)
	}
	return b.String()
}

// Reading states the direction of the comparison in words, so nobody has to
// derive it from the break-even margin: tax on profit is margin × revenue ×
// rate and tax on revenue is ratio × revenue, so a margin above the
// break-even (ratio ÷ rate) means declaring on profit pays more.
func (c *TaxComparison) Reading() string {
	margin, breakEven := FormatPercent(c.Margin, 2), FormatPercent(c.BreakEven, 2)
	switch {
	case math.Abs(c.Difference) < 1:
		return fmt.Sprintf("Biên lợi nhuận thực tế %s bằng biên hòa vốn %s → hai cách nộp gần như bằng nhau.", margin, breakEven)
	case c.Difference > 0:
		return fmt.Sprintf("Biên lợi nhuận thực tế %s cao hơn biên hòa vốn %s → kê khai trên lợi nhuận nộp nhiều hơn "+
			"(cách tính theo %% doanh thu nộp ít hơn); thấp hơn hòa vốn thì ngược lại.", margin, breakEven)
	default:
		return fmt.Sprintf("Biên lợi nhuận thực tế %s thấp hơn biên hòa vốn %s → kê khai trên lợi nhuận nộp ít hơn "+
			"(cách tính theo %% doanh thu nộp nhiều hơn); cao hơn hòa vốn thì ngược lại.", margin, breakEven)
	}
}

// FormatVND writes an amount rounded to the đồng with dots between
// thousands: 1200000000 → "1.200.000.000 đ".
func FormatVND(amount float64) string {
	s := strconv.FormatFloat(math.Abs(amount), 'f', 0, 64)
	var b strings.Builder
	if amount < 0 && s != "0" {
		b.WriteByte('-')
	}
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	b.WriteString(" đ")
	return b.String()
}

// FormatPercent writes a fraction as a percentage with a decimal comma.
// decimals < 0 keeps up to two decimals and drops trailing zeros
// (0.2 → "20%", 0.175 → "17,5%").
func FormatPercent(fraction float64, decimals int) string {
	trim := decimals < 0
	if trim {
		decimals = 2
	}
	s := strconv.FormatFloat(fraction*100, 'f', decimals, 64)
	if trim && strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return strings.Replace(s, ".", ",", 1) + "%"
}

// checkTaxRate accepts a tax rate as a fraction in (0, 1].
func checkTaxRate(rate float64) error {
	if !finite(rate) || rate <= 0 || rate > 1 {
		return errors.New("Thuế suất phải trong khoảng (0, 1], ví dụ 0.2 cho 20%")
	}
	return nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
