package opcvn

import (
	"fmt"
)

// ToolCompareTaxMethods is the agent tool that runs these formulas.
const ToolCompareTaxMethods = "compare_tax_methods"

// Formula ids, as written in domain-packs/opc-vn-business-law/formulas.yaml.
const (
	FormulaTaxOnRevenue = "thue_theo_ty_le_doanh_thu"
	FormulaTaxOnProfit  = "thue_theo_loi_nhuan"
	FormulaBreakEven    = "bien_hoa_von"
	FormulaComparison   = "so_sanh_thue_tndn"
)

// Formulas maps each formula id of the pack to its pure implementation.
// Inputs and outputs use the variable names of formulas.yaml; the revenue
// ratio is in percent, the tax rate a fraction.
func Formulas() map[string]func(map[string]float64) (map[string]float64, error) {
	return map[string]func(map[string]float64) (map[string]float64, error){
		FormulaTaxOnRevenue: func(in map[string]float64) (map[string]float64, error) {
			v, err := need(in, "doanh_thu", "ty_le_doanh_thu")
			if err != nil {
				return nil, err
			}
			if !finite(v[0]) || v[0] <= 0 {
				return nil, fmt.Errorf("Doanh thu phải lớn hơn 0")
			}
			ratio, err := NormalizeRatio(v[1])
			if err != nil {
				return nil, err
			}
			return map[string]float64{"thue_theo_doanh_thu": v[0] * ratio}, nil
		},
		FormulaTaxOnProfit: func(in map[string]float64) (map[string]float64, error) {
			v, err := need(in, "loi_nhuan", "thue_suat")
			if err != nil {
				return nil, err
			}
			if !finite(v[0]) || v[0] < 0 {
				return nil, fmt.Errorf("Lợi nhuận không được âm (nhập 0 nếu lỗ)")
			}
			if err := checkTaxRate(v[1]); err != nil {
				return nil, err
			}
			return map[string]float64{"thue_theo_loi_nhuan": v[0] * v[1]}, nil
		},
		FormulaBreakEven: func(in map[string]float64) (map[string]float64, error) {
			v, err := need(in, "ty_le_doanh_thu", "thue_suat")
			if err != nil {
				return nil, err
			}
			ratio, err := NormalizeRatio(v[0])
			if err != nil {
				return nil, err
			}
			if err := checkTaxRate(v[1]); err != nil {
				return nil, err
			}
			return map[string]float64{"bien_hoa_von": ratio / v[1]}, nil
		},
		FormulaComparison: func(in map[string]float64) (map[string]float64, error) {
			v, err := need(in, "doanh_thu", "loi_nhuan", "ty_le_doanh_thu", "thue_suat")
			if err != nil {
				return nil, err
			}
			c, err := CompareTaxMethods(TaxInput{Revenue: v[0], Profit: v[1], RevenueRatio: v[2], TaxRate: v[3]})
			if err != nil {
				return nil, err
			}
			return map[string]float64{
				"thue_theo_doanh_thu": c.TaxOnRevenue,
				"thue_theo_loi_nhuan": c.TaxOnProfit,
				"bien_loi_nhuan":      c.Margin,
				"bien_hoa_von":        c.BreakEven,
				"chenh_lech":          c.Difference,
			}, nil
		},
	}
}

// need returns the named inputs in order; a missing one is an error, never
// a default.
func need(in map[string]float64, names ...string) ([]float64, error) {
	out := make([]float64, len(names))
	for i, name := range names {
		v, ok := in[name]
		if !ok {
			return nil, fmt.Errorf("thiếu biến %s", name)
		}
		out[i] = v
	}
	return out, nil
}
