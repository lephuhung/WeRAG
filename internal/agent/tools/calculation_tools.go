package tools

// calculationTools are the tools that compute a domain pack's figures and
// return a code-written answer block (types.ToolDataAnswerBlock). A question
// with an amount must go through one of them before the model concludes
// (agent/calc_gate.go). A new tool that sets an answer block belongs here;
// TestCalculationToolsListAnswerBlockTools checks it.
var calculationTools = map[string]bool{
	ToolCalculateHouseholdTax: true,
	ToolCompareTaxMethods:     true,
}

// IsCalculationTool reports whether name is a calculation tool.
func IsCalculationTool(name string) bool { return calculationTools[name] }
