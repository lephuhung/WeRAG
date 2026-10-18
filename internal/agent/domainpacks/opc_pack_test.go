package domainpacks

import (
	"bytes"
	"testing"

	domainpackassets "github.com/Tencent/WeKnora/domain-packs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pilot pack (docs/domain-pack-standard.md §10) ships, loads and keeps
// the contract the reconcile command and the eval suite rely on.
func TestOpcVNBusinessLawPackShips(t *testing.T) {
	reg, err := Load(domainpackassets.FS)
	require.NoError(t, err)
	pack, ok := reg.Get("opc-vn-business-law")
	require.True(t, ok, "pack must be embedded")

	m := pack.Manifest.Metadata
	assert.Equal(t, "1.1.0", m.Version)
	assert.Equal(t, "17/10/2026", m.AsOf.VN())
	assert.Equal(t, "2027-04-17", m.ReviewBy.Format(dateLayout))
	assert.Equal(t, "CC-BY-4.0", m.Licence)
	assert.Equal(t, []string{"search_knowledge", "compare_tax_methods"}, m.RequiredTools)
	require.Len(t, m.DraftDocuments, 3)
	assert.Equal(t, "Lần 2", m.DraftDocuments[0].Version)
	assert.Len(t, pack.Catalogue().Documents, 13)
	assert.Empty(t, pack.Lint())
	assert.Len(t, pack.Formulas(), 4)
	assert.Contains(t, pack.CitedIn("59/2020/QH14"), "SKILL.md")
	block := reg.FormulaBlock("compare_tax_methods")
	assert.Contains(t, block, "Công thức áp dụng:\n")
	assert.Contains(t, block, "Thuế theo % doanh thu = Doanh thu × Tỷ lệ % doanh thu (căn cứ: 67/2025/QH15)")

	evals, err := domainpackassets.FS.ReadFile("opc-vn-business-law/evals/cases.jsonl")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, bytes.Count(bytes.TrimSpace(evals), []byte("\n"))+1, 20)
}
