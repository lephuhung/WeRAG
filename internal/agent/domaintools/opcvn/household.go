package opcvn

import (
	"errors"
	"fmt"
	"strings"
)

// ToolHouseholdTax is the agent tool that runs the household-business
// formulas below.
const ToolHouseholdTax = "calculate_household_tax"

// Household-business formula ids, as written in formulas.yaml.
const (
	FormulaHouseholdVAT        = "gtgt_ho_kinh_doanh"
	FormulaHouseholdPITRevenue = "tncn_ho_theo_doanh_thu"
	FormulaHouseholdPITProfit  = "tncn_ho_theo_loi_nhuan"
	FormulaPITReduction        = "giam_thue_tncn"
	FormulaHouseholdBreakEven  = "loi_nhuan_hoa_von_ho"
)

// HouseholdNotIncluded is what the calculation leaves out; it is part of
// every assumptions block.
const HouseholdNotIncluded = "Chưa tính lệ phí môn bài, thuế tài nguyên, thuế tiêu thụ đặc biệt, bảo vệ môi trường, thuế cho thuê bất động sản."

// HouseholdInput are the figures of a household business (hộ kinh doanh,
// cá nhân kinh doanh). Revenue comes from the user; the threshold, ratios
// and rates come from the tax documents found in the knowledge base. No
// legal parameter has a default.
type HouseholdInput struct {
	// Revenue is the annual revenue in đồng (doanh_thu).
	Revenue float64
	// Threshold is the annual revenue up to which no VAT or PIT is due, in
	// đồng (nguong_doanh_thu); the PIT revenue base is what exceeds it.
	Threshold float64
	// VATRatio and PITRatio are the ratios on revenue in PERCENT
	// (ty_le_gtgt, ty_le_tncn): 1 means 1 %, 0.5 means 0.5 %.
	VATRatio, PITRatio float64
	// RevenueMethodCap is the annual revenue up to which PIT may be paid as
	// a ratio of revenue, in đồng (tran_doanh_thu_theo_doanh_thu); above it
	// only PIT on profit applies.
	RevenueMethodCap float64
	// Cost (chi_phi, đồng) and ProfitRate (thue_suat_tncn, fraction) are
	// set to compare with PIT on profit; ProfitRate alone gives the
	// break-even profit.
	Cost, ProfitRate *float64
	// Reduction is a PIT reduction in PERCENT of the tax (ty_le_giam_tncn).
	Reduction *float64
	// PeriodRevenue is the revenue of one quarter or month (doanh_thu_ky).
	PeriodRevenue *float64
}

// HouseholdTax is the result.
type HouseholdTax struct {
	In              HouseholdInput
	Liable          bool    // revenue above the threshold
	RevenueMethod   bool    // PIT as a ratio of revenue allowed (revenue ≤ cap)
	VAT             float64 // thue_gtgt
	PITRevenue      float64 // thue_tncn_doanh_thu (before reduction)
	PITProfit       float64 // thue_tncn_loi_nhuan (before reduction), when HasProfit
	HasProfit       bool
	PITRevenueNet   float64 // after Reduction
	PITProfitNet    float64
	BreakEvenProfit float64 // loi_nhuan_hoa_von, when HasBreakEven
	HasBreakEven    bool
	PeriodVAT       float64 // GTGT of PeriodRevenue, when set
	Assumptions     []string
}

// CalculateHouseholdTax computes VAT and PIT of a household business: VAT
// as a ratio of the whole revenue, PIT as a ratio of the revenue above the
// threshold, and — given costs and a rate — PIT on profit, with a PIT
// reduction applied to both.
func CalculateHouseholdTax(in HouseholdInput) (*HouseholdTax, error) {
	switch {
	case !finite(in.Revenue) || in.Revenue <= 0:
		return nil, errors.New("Doanh thu phải lớn hơn 0")
	case in.Revenue > maxAmount:
		return nil, fmt.Errorf("Doanh thu vượt giới hạn %s; kiểm tra lại đơn vị (đồng)", FormatVND(maxAmount))
	case !finite(in.Threshold) || in.Threshold < 0 || in.Threshold > maxAmount:
		return nil, errors.New("Ngưỡng doanh thu phải là số đồng không âm (ví dụ 1 tỷ = 1000000000)")
	}
	vat, err := householdRatio("Tỷ lệ GTGT", in.VATRatio)
	if err != nil {
		return nil, err
	}
	pit, err := householdRatio("Tỷ lệ TNCN", in.PITRatio)
	if err != nil {
		return nil, err
	}
	if !finite(in.RevenueMethodCap) || in.RevenueMethodCap < in.Threshold || in.RevenueMethodCap > maxAmount {
		return nil, errors.New("Mức doanh thu tối đa được tính TNCN theo % doanh thu phải là số đồng, không nhỏ hơn ngưỡng (ví dụ 3 tỷ = 3000000000)")
	}
	reduction := 0.0
	if in.Reduction != nil {
		if r := *in.Reduction; !finite(r) || r < 0 || r >= 100 {
			return nil, errors.New("Tỷ lệ giảm thuế TNCN ghi theo phần trăm, từ 0 đến dưới 100 (ví dụ 30 cho 30%)")
		}
		reduction = *in.Reduction / 100
	}
	if in.ProfitRate != nil {
		if err := checkTaxRate(*in.ProfitRate); err != nil {
			return nil, fmt.Errorf("Thuế suất TNCN trên lợi nhuận: %w", err)
		}
	}
	if in.Cost != nil {
		if c := *in.Cost; !finite(c) || c < 0 || c > maxAmount {
			return nil, errors.New("Chi phí phải là số đồng không âm")
		}
		if in.ProfitRate == nil {
			return nil, errors.New("Có chi phí thì cần cả thuế suất TNCN trên lợi nhuận (thue_suat_tncn) tra trong kho tri thức")
		}
	}
	if in.PeriodRevenue != nil {
		if p := *in.PeriodRevenue; !finite(p) || p < 0 || p > in.Revenue {
			return nil, errors.New("Doanh thu kỳ phải không âm và không lớn hơn doanh thu năm")
		}
	}

	out := &HouseholdTax{In: in, Liable: in.Revenue > in.Threshold, RevenueMethod: in.Revenue <= in.RevenueMethodCap}
	if out.Liable && !out.RevenueMethod && (in.Cost == nil || in.ProfitRate == nil) {
		return nil, fmt.Errorf("Doanh thu năm trên %s chỉ được tính TNCN trên lợi nhuận: cần chi phí (chi_phi) từ người dùng và thuế suất (thue_suat_tncn) tra trong kho tri thức",
			FormatVND(in.RevenueMethodCap))
	}
	if out.Liable {
		out.VAT = in.Revenue * vat
		if out.RevenueMethod {
			out.PITRevenue = (in.Revenue - in.Threshold) * pit
		}
		if in.PeriodRevenue != nil {
			out.PeriodVAT = *in.PeriodRevenue * vat
		}
		if in.Cost != nil {
			out.HasProfit = true
			if profit := in.Revenue - *in.Cost; profit > 0 {
				out.PITProfit = profit * *in.ProfitRate
			}
		}
		if in.ProfitRate != nil && out.RevenueMethod {
			out.HasBreakEven = true
			out.BreakEvenProfit = out.PITRevenue / *in.ProfitRate
		}
	}
	out.PITRevenueNet = out.PITRevenue * (1 - reduction)
	out.PITProfitNet = out.PITProfit * (1 - reduction)
	out.Assumptions = householdAssumptions(out, vat, pit, reduction)
	return out, nil
}

// householdRatio checks a ratio given in percent. The lowest legal ratio is
// 0,5 %: a value under 0,1 is a fraction typed by mistake (0.01 for 1 %).
func householdRatio(name string, percent float64) (float64, error) {
	if finite(percent) && percent > 0 && percent < 0.1 {
		return 0, fmt.Errorf("%s ghi theo PHẦN TRĂM (1 cho 1%%, 0.5 cho 0,5%%); %v quá nhỏ", name, percent)
	}
	r, err := NormalizeRatio(percent)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return r, nil
}

func householdAssumptions(c *HouseholdTax, vat, pit, reduction float64) []string {
	in := c.In
	a := []string{
		"Doanh thu năm " + FormatVND(in.Revenue) + " là số người hỏi cung cấp, chưa được kiểm tra; là doanh thu của cả năm, gồm cả doanh thu đã được khấu trừ, nộp thay.",
		"Ngưỡng doanh thu " + FormatVND(in.Threshold) + ", tỷ lệ GTGT " + FormatPercent(vat, -1) + " và tỷ lệ TNCN " + FormatPercent(pit, -1) +
			" là tham số tra trong văn bản thuế (theo ngành nghề); phải đối chiếu văn bản hiện hành tại thời điểm kê khai.",
	}
	if in.Cost != nil {
		a = append(a, "Chi phí "+FormatVND(*in.Cost)+" coi là toàn bộ chi phí được trừ, có hóa đơn, chứng từ; thuế suất TNCN trên lợi nhuận "+
			FormatPercent(*in.ProfitRate, -1)+" theo mức doanh thu năm.")
	}
	if reduction > 0 {
		a = append(a, "Giảm "+FormatPercent(reduction, -1)+" số thuế TNCN phải nộp chỉ áp dụng khi đủ điều kiện của văn bản giảm thuế (kỳ tính thuế, mức doanh thu năm tối đa); kiểm tra điều kiện trước khi dùng số sau giảm.")
	}
	a = append(a,
		"Phương pháp tính TNCN theo % doanh thu chỉ được chọn khi doanh thu năm không quá "+FormatVND(in.RevenueMethodCap)+" (mức tra trong văn bản); vượt thì chỉ tính trên lợi nhuận.",
		"Một ngành nghề, một tỷ lệ: hộ có nhiều ngành nghề tính riêng từng ngành, mức trừ ngưỡng chỉ trừ một lần trong năm.",
		"Tạm nộp theo quý/tháng: GTGT tính trên doanh thu của kỳ; mức trừ "+FormatVND(in.Threshold)+" tính cho cả năm, nên TNCN của từng kỳ không lấy số năm chia đều.",
		HouseholdNotIncluded,
	)
	return a
}

// Render is the report the model reads and the answer block.
func (c *HouseholdTax) Render() string {
	in := c.In
	var b strings.Builder
	b.WriteString("Thuế hộ kinh doanh, cá nhân kinh doanh (GTGT và TNCN)\n")
	row := func(k, v string) { fmt.Fprintf(&b, "- %s: %s\n", k, v) }
	row("Doanh thu năm", FormatVND(in.Revenue))
	row("Ngưỡng doanh thu không phải nộp GTGT, TNCN", FormatVND(in.Threshold))
	if !c.Liable {
		b.WriteString("\nĐọc kết quả: doanh thu năm không vượt ngưỡng → không phải nộp thuế GTGT và thuế TNCN; " +
			"hộ kinh doanh thông báo doanh thu theo quy định thay vì khai thuế.\n")
	} else {
		row("Thuế GTGT ("+FormatPercent(in.VATRatio/100, -1)+" × doanh thu)", FormatVND(c.VAT))
		if c.RevenueMethod {
			row("Thuế TNCN theo % doanh thu ("+FormatPercent(in.PITRatio/100, -1)+" × (doanh thu − ngưỡng))", FormatVND(c.PITRevenue))
			if in.Reduction != nil && *in.Reduction > 0 {
				row("Thuế TNCN theo % doanh thu sau giảm "+FormatPercent(*in.Reduction/100, -1), FormatVND(c.PITRevenueNet))
			}
		} else {
			row("Thuế TNCN theo % doanh thu", "không áp dụng (doanh thu năm trên "+FormatVND(in.RevenueMethodCap)+")")
		}
		if c.HasProfit {
			row("Thuế TNCN trên lợi nhuận ("+FormatPercent(*in.ProfitRate, -1)+" × (doanh thu − chi phí))", FormatVND(c.PITProfit))
			if in.Reduction != nil && *in.Reduction > 0 {
				row("Thuế TNCN trên lợi nhuận sau giảm", FormatVND(c.PITProfitNet))
			}
		}
		if c.HasBreakEven {
			row("Lợi nhuận hòa vốn giữa hai phương pháp TNCN", FormatVND(c.BreakEvenProfit))
		}
		if c.RevenueMethod {
			row("Tổng GTGT + TNCN cả năm (TNCN theo % doanh thu)", FormatVND(c.VAT+c.PITRevenueNet))
		}
		if c.HasProfit {
			row("Tổng GTGT + TNCN cả năm (TNCN trên lợi nhuận)", FormatVND(c.VAT+c.PITProfitNet))
		}
		if in.PeriodRevenue != nil {
			row("Thuế GTGT của kỳ (doanh thu kỳ "+FormatVND(*in.PeriodRevenue)+")", FormatVND(c.PeriodVAT))
		}
		fmt.Fprintf(&b, "\nĐọc kết quả: %s\n", c.Reading())
	}
	b.WriteString("\nGiả định:\n")
	for _, a := range c.Assumptions {
		fmt.Fprintf(&b, "- %s\n", a)
	}
	return b.String()
}

// Reading states which PIT method pays less, in words.
func (c *HouseholdTax) Reading() string {
	switch {
	case !c.RevenueMethod:
		return "doanh thu năm vượt mức được tính TNCN theo % doanh thu, nên TNCN tính trên lợi nhuận; thuế GTGT vẫn tính theo % trên toàn bộ doanh thu."
	case c.HasProfit && c.PITProfit < c.PITRevenue:
		return "thuế TNCN trên lợi nhuận thấp hơn thuế TNCN theo % doanh thu; phương pháp lợi nhuận cần sổ sách, chứng từ chi phí và giữ ổn định 02 năm."
	case c.HasProfit && c.PITProfit > c.PITRevenue:
		return "thuế TNCN theo % doanh thu thấp hơn thuế TNCN trên lợi nhuận."
	case c.HasProfit:
		return "hai phương pháp TNCN cho số thuế như nhau."
	case c.HasBreakEven:
		return "lợi nhuận cả năm (doanh thu − chi phí) thấp hơn mức hòa vốn thì tính TNCN trên lợi nhuận nộp ít hơn; cao hơn thì theo % doanh thu nộp ít hơn. Thuế GTGT như nhau ở cả hai phương pháp."
	default:
		return "thuế GTGT tính trên toàn bộ doanh thu; thuế TNCN theo % doanh thu chỉ tính trên phần vượt ngưỡng."
	}
}

// HouseholdFormulas maps each household formula id of the pack to its
// pure implementation (variable names of formulas.yaml).
func HouseholdFormulas() map[string]func(map[string]float64) (map[string]float64, error) {
	base := func(in map[string]float64) (HouseholdInput, error) {
		v, err := need(in, "doanh_thu", "nguong_doanh_thu")
		if err != nil {
			return HouseholdInput{}, err
		}
		// each formula sets the ratio (and cap) it uses; the others only
		// have to pass validation and are not read
		return HouseholdInput{Revenue: v[0], Threshold: v[1], VATRatio: 1, PITRatio: 1, RevenueMethodCap: maxAmount}, nil
	}
	return map[string]func(map[string]float64) (map[string]float64, error){
		FormulaHouseholdVAT: func(in map[string]float64) (map[string]float64, error) {
			h, err := base(in)
			if err != nil {
				return nil, err
			}
			v, err := need(in, "ty_le_gtgt")
			if err != nil {
				return nil, err
			}
			h.VATRatio = v[0]
			c, err := CalculateHouseholdTax(h)
			if err != nil {
				return nil, err
			}
			return map[string]float64{"thue_gtgt": c.VAT}, nil
		},
		FormulaHouseholdPITRevenue: func(in map[string]float64) (map[string]float64, error) {
			h, err := base(in)
			if err != nil {
				return nil, err
			}
			v, err := need(in, "ty_le_tncn", "tran_doanh_thu_theo_doanh_thu")
			if err != nil {
				return nil, err
			}
			h.PITRatio, h.RevenueMethodCap = v[0], v[1]
			c, err := CalculateHouseholdTax(h)
			if err != nil {
				return nil, err
			}
			return map[string]float64{"thue_tncn_doanh_thu": c.PITRevenue}, nil
		},
		FormulaHouseholdPITProfit: func(in map[string]float64) (map[string]float64, error) {
			h, err := base(in)
			if err != nil {
				return nil, err
			}
			v, err := need(in, "chi_phi", "thue_suat_tncn")
			if err != nil {
				return nil, err
			}
			h.Cost, h.ProfitRate = &v[0], &v[1]
			c, err := CalculateHouseholdTax(h)
			if err != nil {
				return nil, err
			}
			return map[string]float64{"thue_tncn_loi_nhuan": c.PITProfit}, nil
		},
		FormulaPITReduction: func(in map[string]float64) (map[string]float64, error) {
			v, err := need(in, "thue_tncn", "ty_le_giam_tncn")
			if err != nil {
				return nil, err
			}
			if !finite(v[0]) || v[0] < 0 {
				return nil, errors.New("Thuế TNCN không được âm")
			}
			if !finite(v[1]) || v[1] < 0 || v[1] >= 100 {
				return nil, errors.New("Tỷ lệ giảm ghi theo phần trăm, từ 0 đến dưới 100")
			}
			return map[string]float64{"thue_tncn_sau_giam": v[0] * (1 - v[1]/100)}, nil
		},
		FormulaHouseholdBreakEven: func(in map[string]float64) (map[string]float64, error) {
			h, err := base(in)
			if err != nil {
				return nil, err
			}
			v, err := need(in, "ty_le_tncn", "thue_suat_tncn", "tran_doanh_thu_theo_doanh_thu")
			if err != nil {
				return nil, err
			}
			h.PITRatio, h.ProfitRate, h.RevenueMethodCap = v[0], &v[1], v[2]
			c, err := CalculateHouseholdTax(h)
			if err != nil {
				return nil, err
			}
			return map[string]float64{"loi_nhuan_hoa_von": c.BreakEvenProfit}, nil
		},
	}
}
