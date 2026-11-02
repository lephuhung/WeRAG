package opcvn

import (
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

// The session that showed the bug: retail, 2,4 tỷ a year, 600 triệu in Q1.
func TestHouseholdTaxRetail(t *testing.T) {
	c, err := CalculateHouseholdTax(HouseholdInput{
		Revenue: 2_400_000_000, Threshold: 1_000_000_000, VATRatio: 1, PITRatio: 0.5, RevenueMethodCap: 3_000_000_000,
		ProfitRate: f(0.15), Reduction: f(30), ReductionCap: f(3_000_000_000), PeriodRevenue: f(600_000_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string][2]float64{
		"VAT":             {c.VAT, 24_000_000},
		"PITRevenue":      {c.PITRevenue, 7_000_000},
		"PITRevenueNet":   {c.PITRevenueNet, 4_900_000},
		"BreakEvenProfit": {c.BreakEvenProfit, 7_000_000 / 0.15},
		"PeriodVAT":       {c.PeriodVAT, 6_000_000},
	}
	for name, v := range checks {
		if d := v[0] - v[1]; d > 0.5 || d < -0.5 {
			t.Errorf("%s = %v, want %v", name, v[0], v[1])
		}
	}
	out := c.Render()
	for _, want := range []string{"Thuế GTGT cả năm (1% × doanh thu năm): 24.000.000 đ", "4.900.000 đ", "Tổng GTGT + TNCN cả năm (TNCN theo % doanh thu): 28.900.000 đ", "Giả định:", "không lấy số năm chia đều",
		"Thuế TNCN của kỳ: không tính theo kỳ bằng cách chia số cả năm"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
}

func TestHouseholdTaxBelowThreshold(t *testing.T) {
	c, err := CalculateHouseholdTax(HouseholdInput{Revenue: 900_000_000, Threshold: 1_000_000_000, VATRatio: 1, PITRatio: 0.5, RevenueMethodCap: 3_000_000_000, Cost: f(100), ProfitRate: f(0.15)})
	if err != nil {
		t.Fatal(err)
	}
	if c.Liable || c.VAT != 0 || c.PITRevenue != 0 || c.PITProfit != 0 {
		t.Fatalf("below threshold: %+v", c)
	}
	if !strings.Contains(c.Render(), "không phải nộp thuế GTGT và thuế TNCN") {
		t.Fatal(c.Render())
	}
}

func TestHouseholdTaxProfitMethod(t *testing.T) {
	c, err := CalculateHouseholdTax(HouseholdInput{Revenue: 2_400_000_000, Threshold: 1_000_000_000, VATRatio: 1, PITRatio: 0.5, RevenueMethodCap: 3_000_000_000, Cost: f(2_380_000_000), ProfitRate: f(0.15)})
	if err != nil {
		t.Fatal(err)
	}
	if c.PITProfit != 3_000_000 || !strings.Contains(c.Reading(), "lợi nhuận thấp hơn") {
		t.Fatalf("%v %s", c.PITProfit, c.Reading())
	}
	// a loss pays no PIT on profit
	c, _ = CalculateHouseholdTax(HouseholdInput{Revenue: 2_400_000_000, Threshold: 1_000_000_000, VATRatio: 1, PITRatio: 0.5, RevenueMethodCap: 3_000_000_000, Cost: f(3_000_000_000), ProfitRate: f(0.15)})
	if c.PITProfit != 0 {
		t.Fatal(c.PITProfit)
	}
}

func TestHouseholdTaxRefusesBadInput(t *testing.T) {
	bad := []HouseholdInput{
		{Revenue: 0, Threshold: 1, VATRatio: 1, PITRatio: 1, RevenueMethodCap: 5},
		{Revenue: 1, Threshold: -1, VATRatio: 1, PITRatio: 1, RevenueMethodCap: 5},
		{Revenue: 1, Threshold: 1, VATRatio: 100, PITRatio: 1, RevenueMethodCap: 5},
		{Revenue: 1, Threshold: 1, VATRatio: 1, PITRatio: 0, RevenueMethodCap: 5},
		{Revenue: 1, Threshold: 1, VATRatio: 1, PITRatio: 1, RevenueMethodCap: 5, Cost: f(1)},
		{Revenue: 1, Threshold: 1, VATRatio: 1, PITRatio: 1, RevenueMethodCap: 5, Reduction: f(100)},
		{Revenue: 1, Threshold: 1, VATRatio: 1, PITRatio: 1, RevenueMethodCap: 5, ProfitRate: f(15)},
		{Revenue: 10, Threshold: 1, VATRatio: 1, PITRatio: 1, RevenueMethodCap: 50, PeriodRevenue: f(11)},
		// a fraction typed for a percent, a cap below the threshold, above
		// the cap without profit figures
		{Revenue: 10, Threshold: 1, VATRatio: 0.01, PITRatio: 1, RevenueMethodCap: 50},
		{Revenue: 10, Threshold: 5, VATRatio: 1, PITRatio: 1, RevenueMethodCap: 4},
		{Revenue: 10, Threshold: 1, VATRatio: 1, PITRatio: 1, RevenueMethodCap: 5},
	}
	for i, in := range bad {
		if _, err := CalculateHouseholdTax(in); err == nil {
			t.Errorf("case %d accepted: %+v", i, in)
		}
	}
}

func TestHouseholdTaxAboveRevenueMethodCap(t *testing.T) {
	c, err := CalculateHouseholdTax(HouseholdInput{Revenue: 5_000_000_000, Threshold: 1_000_000_000, VATRatio: 1, PITRatio: 0.5,
		RevenueMethodCap: 3_000_000_000, Cost: f(4_000_000_000), ProfitRate: f(0.17)})
	if err != nil {
		t.Fatal(err)
	}
	if c.RevenueMethod || c.PITRevenue != 0 || c.HasBreakEven || c.PITProfit != 170_000_000 {
		t.Fatalf("%+v", c)
	}
	out := c.Render()
	for _, want := range []string{"không áp dụng (doanh thu năm trên 3.000.000.000 đ)", "Tổng GTGT + TNCN cả năm (TNCN trên lợi nhuận): 220.000.000 đ"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "TNCN theo % doanh thu thấp hơn") || strings.Contains(out, "(TNCN theo % doanh thu): ") {
		t.Fatal(out)
	}
}

// Fable review: the reduction applies only up to its revenue cap, which is
// required with it; no named fee, no legal period written in code.
func TestHouseholdTaxReductionCap(t *testing.T) {
	in := HouseholdInput{Revenue: 2_400_000_000, Threshold: 1_000_000_000, VATRatio: 1, PITRatio: 0.5, RevenueMethodCap: 3_000_000_000, Reduction: f(30)}
	if _, err := CalculateHouseholdTax(in); err == nil {
		t.Fatal("a reduction without its cap was accepted")
	}
	in.ReductionCap = f(2_000_000_000)
	c, err := CalculateHouseholdTax(in)
	if err != nil {
		t.Fatal(err)
	}
	if c.Reduced || c.PITRevenueNet != c.PITRevenue {
		t.Fatalf("above the cap the tax is not reduced: %+v", c)
	}
	out := c.Render()
	if !strings.Contains(out, "Giảm thuế TNCN: không áp dụng (doanh thu năm trên 2.000.000.000 đ)") || strings.Contains(out, "sau giảm") {
		t.Fatalf("render:\n%s", out)
	}
	in.ReductionCap = f(3_000_000_000)
	if c, _ = CalculateHouseholdTax(in); !c.Reduced || c.PITRevenueNet != 4_900_000 {
		t.Fatalf("under the cap: %+v", c)
	}
	if strings.Contains(HouseholdNotIncluded, "môn bài") {
		t.Fatal("the assumptions name a fee")
	}
	c, _ = CalculateHouseholdTax(HouseholdInput{Revenue: 2_400_000_000, Threshold: 1_000_000_000, VATRatio: 1, PITRatio: 0.5, RevenueMethodCap: 3_000_000_000, Cost: f(2_380_000_000), ProfitRate: f(0.15)})
	if strings.Contains(c.Reading(), "02 năm") {
		t.Fatal("a legal period is written in code")
	}
	got, err := HouseholdFormulas()[FormulaPITReduction](map[string]float64{"thue_tncn": 7_000_000, "ty_le_giam_tncn": 30, "doanh_thu": 3_500_000_000, "tran_doanh_thu_giam_tncn": 3_000_000_000})
	if err != nil || got["thue_tncn_sau_giam"] != 7_000_000 {
		t.Fatalf("formula above the cap: %v %v", got, err)
	}
}

// The cap stays mandatory; an uncapped relief is an explicit choice that
// excludes the cap.
func TestHouseholdTaxUncappedReduction(t *testing.T) {
	in := HouseholdInput{Revenue: 12_000_000_000, Threshold: 1_000_000_000, VATRatio: 1, PITRatio: 0.5, RevenueMethodCap: 20_000_000_000, Reduction: f(30), ReductionUncapped: true}
	c, err := CalculateHouseholdTax(in)
	if err != nil || !c.Reduced {
		t.Fatalf("%v %+v", err, c)
	}
	in.ReductionCap = f(10_000_000_000)
	if _, err := CalculateHouseholdTax(in); err == nil {
		t.Fatal("cap and uncapped together were accepted")
	}
}
