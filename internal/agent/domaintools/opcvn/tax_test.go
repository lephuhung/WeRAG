package opcvn

import (
	"math"
	"strings"
	"testing"
)

// The first three cases are self_test() of scripts/so_sanh_thue.py
// (source skill opc-vn-business-law, Diginno, MIT), with the ratio written
// in percent (the script's 0.015 is 1.5 here, its 0.05 is 5).
func TestCompareTaxMethodsReproducesScriptSelfTest(t *testing.T) {
	k, err := CompareTaxMethods(TaxInput{Revenue: 8_000_000_000, Profit: 1_200_000_000, RevenueRatio: 1.5, TaxRate: 0.20})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(k.TaxOnRevenue-120_000_000) >= 1 {
		t.Errorf("thue_theo_doanh_thu = %v", k.TaxOnRevenue)
	}
	if math.Abs(k.TaxOnProfit-240_000_000) >= 1 {
		t.Errorf("thue_theo_loi_nhuan = %v", k.TaxOnProfit)
	}
	if math.Abs(k.BreakEven-0.075) >= 1e-9 {
		t.Errorf("bien_hoa_von = %v", k.BreakEven)
	}
	if !strings.Contains(k.Recommendation, "% doanh thu") || k.Recommendation != RecommendRevenue {
		t.Errorf("khuyen_nghi = %q", k.Recommendation)
	}
	if math.Abs(k.Margin-0.15) >= 1e-12 || math.Abs(k.Difference-120_000_000) >= 1 {
		t.Errorf("margin %v difference %v", k.Margin, k.Difference)
	}

	k2, err := CompareTaxMethods(TaxInput{Revenue: 8_000_000_000, Profit: 1_200_000_000, RevenueRatio: 1.5, TaxRate: 0.20})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(k2.RevenueRatio-0.015) >= 1e-12 {
		t.Errorf("percent form: ty_le = %v", k2.RevenueRatio)
	}
	// 0.5 is half a percent, never 50 %
	k4, err := CompareTaxMethods(TaxInput{Revenue: 1_000_000_000, Profit: 100_000_000, RevenueRatio: 0.5, TaxRate: 0.20})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(k4.TaxOnRevenue-5_000_000) >= 1 {
		t.Errorf("0.5 must mean 0,5 %%: thue_theo_doanh_thu = %v", k4.TaxOnRevenue)
	}

	k3, err := CompareTaxMethods(TaxInput{Revenue: 1_000_000_000, Profit: 100_000_000, RevenueRatio: 5, TaxRate: 0.20})
	if err != nil {
		t.Fatal(err)
	}
	if k3.Difference >= 0 || !strings.Contains(k3.Recommendation, "lợi nhuận") || k3.Recommendation != RecommendDeclared {
		t.Errorf("k3: difference %v, %q", k3.Difference, k3.Recommendation)
	}
}

func TestCompareTaxMethodsEquivalent(t *testing.T) {
	// 1 % of 10 tỷ = 100 tr = 20 % of 500 tr
	k, err := CompareTaxMethods(TaxInput{Revenue: 10_000_000_000, Profit: 500_000_000, RevenueRatio: 1, TaxRate: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	if k.Recommendation != RecommendEquivalent {
		t.Errorf("got %q", k.Recommendation)
	}
}

func TestCompareTaxMethodsRejectsBadInput(t *testing.T) {
	bad := []TaxInput{
		// the script's five
		{Revenue: 0, Profit: 0, RevenueRatio: 0.01, TaxRate: 0.2},
		{Revenue: 100, Profit: -1, RevenueRatio: 0.01, TaxRate: 0.2},
		{Revenue: 100, Profit: 200, RevenueRatio: 0.01, TaxRate: 0.2},
		{Revenue: 100, Profit: 10, RevenueRatio: 0, TaxRate: 0.2},
		{Revenue: 100, Profit: 10, RevenueRatio: 0.01, TaxRate: 0},
		// ratio of 100 % or more, rate above 1, non-finite, absurd amounts
		{Revenue: 100, Profit: 10, RevenueRatio: 100, TaxRate: 0.2},
		{Revenue: 100, Profit: 10, RevenueRatio: 250, TaxRate: 0.2},
		{Revenue: 100, Profit: 10, RevenueRatio: 0.01, TaxRate: 20},
		{Revenue: math.NaN(), Profit: 10, RevenueRatio: 0.01, TaxRate: 0.2},
		{Revenue: 100, Profit: 10, RevenueRatio: math.Inf(1), TaxRate: 0.2},
		{Revenue: 2e15, Profit: 10, RevenueRatio: 0.01, TaxRate: 0.2},
	}
	for i, in := range bad {
		if _, err := CompareTaxMethods(in); err == nil {
			t.Errorf("case %d (%+v): want an error", i, in)
		}
	}
}

func TestRenderHasFiguresAndAssumptions(t *testing.T) {
	k, err := CompareTaxMethods(TaxInput{Revenue: 8e9, Profit: 1.2e9, RevenueRatio: 1.5, TaxRate: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	out := k.Render()
	for _, want := range []string{
		"Doanh thu: 8.000.000.000 đ",
		"Lợi nhuận trước thuế: 1.200.000.000 đ",
		"Biên lợi nhuận: 15,00%",
		"Tỷ lệ % doanh thu: 1,50%",
		"Thuế suất TNDN: 20%",
		"Thuế theo % doanh thu: 120.000.000 đ",
		"Thuế theo lợi nhuận (kê khai): 240.000.000 đ",
		"Chênh lệch (kê khai − % doanh thu): 120.000.000 đ",
		"Biên lợi nhuận hòa vốn: 7,50%",
		"Khuyến nghị sơ bộ: " + RecommendRevenue,
		"Giả định:",
		NotIncluded,
		"văn bản thuế hiện hành",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
}

func TestFormatters(t *testing.T) {
	for in, want := range map[float64]string{
		0: "0 đ", 999: "999 đ", 1000: "1.000 đ", 1_200_000_000: "1.200.000.000 đ",
		-120_000_000: "-120.000.000 đ", 1234.6: "1.235 đ", -0.4: "0 đ",
	} {
		if got := FormatVND(in); got != want {
			t.Errorf("FormatVND(%v) = %q, want %q", in, got, want)
		}
	}
	if got := FormatPercent(0.175, -1); got != "17,5%" {
		t.Errorf("got %q", got)
	}
	if got := FormatPercent(0.2, -1); got != "20%" {
		t.Errorf("got %q", got)
	}
	if got := FormatPercent(0.015, 2); got != "1,50%" {
		t.Errorf("got %q", got)
	}
}

func TestReadingStatesTheDirection(t *testing.T) {
	// tax-2 of the eval: 6 % margin, 1 % ratio, 17 % rate → break-even 5,88 %
	k, err := CompareTaxMethods(TaxInput{Revenue: 5_000_000_000, Profit: 300_000_000, RevenueRatio: 1, TaxRate: 0.17})
	if err != nil {
		t.Fatal(err)
	}
	want := "Biên lợi nhuận thực tế 6,00% cao hơn biên hòa vốn 5,88% → kê khai trên lợi nhuận nộp nhiều hơn"
	if !strings.HasPrefix(k.Reading(), want) || k.Recommendation != RecommendRevenue {
		t.Errorf("reading %q, recommendation %q", k.Reading(), k.Recommendation)
	}
	if !strings.Contains(k.Render(), "Đọc kết quả: "+want) {
		t.Errorf("render lacks the reading:\n%s", k.Render())
	}
	low, _ := CompareTaxMethods(TaxInput{Revenue: 1_000_000_000, Profit: 20_000_000, RevenueRatio: 1, TaxRate: 0.2})
	if !strings.Contains(low.Reading(), "thấp hơn biên hòa vốn 5,00% → kê khai trên lợi nhuận nộp ít hơn") ||
		low.Recommendation != RecommendDeclared {
		t.Errorf("low margin: %q / %q", low.Reading(), low.Recommendation)
	}
	eq, _ := CompareTaxMethods(TaxInput{Revenue: 10_000_000_000, Profit: 500_000_000, RevenueRatio: 1, TaxRate: 0.2})
	if !strings.Contains(eq.Reading(), "gần như bằng nhau") {
		t.Errorf("equal: %q", eq.Reading())
	}
}
