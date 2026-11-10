package vbpl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficialTitleFromRealPushes(t *testing.T) {
	cases := []struct{ file, docType, number, want string }{
		{"tvpl_15_2026_nd_cp_head.html", "Nghị định", "15/2026/NĐ-CP",
			"Nghị định số 15/2026/NĐ-CP Sửa đổi, bổ sung một số điều của các Nghị định để cắt giảm, đơn giản hóa thủ tục hành chính liên quan đến hoạt động sản xuất, kinh doanh thuộc phạm vi quản lý nhà nước của Bộ Khoa học và Công nghệ"},
		{"tvpl_229_2025_nd_cp_head.html", "Nghị định", "229/2025/NĐ-CP",
			"Nghị định số 229/2025/NĐ-CP Sửa đổi, bổ sung khoản 3 Điều 87 của Nghị định số 63/2023/NĐ-CP ngày 18 tháng 8 năm 2023 của Chính phủ quy định chi tiết một số điều của Luật Tần số vô tuyến điện số 42/2009/QH12, được sửa đổi, bổ sung một số điều theo Luật số 09/2022/QH15"},
		// vbpl.vn's own title of this one: "Thông tư số 74/2025/TT-BTC Quy định
		// thời hạn lưu trữ hồ sơ, tài liệu ngành Tài chính"
		{"tvpl_74_2025_tt_btc_head.html", "Thông tư", "74/2025/TT-BTC",
			"Thông tư số 74/2025/TT-BTC Quy định thời hạn lưu trữ hồ sơ, tài liệu ngành Tài chính"},
		{"tvpl_348_2026_nd_cp_head.html", "Nghị định", "348/2026/NĐ-CP",
			"Nghị định số 348/2026/NĐ-CP Quy định về điều kiện cơ sở đào tạo, huấn luyện và tổ chức tuyển dụng, cung ứng thuyền viên hàng hải"},
	}
	for _, c := range cases {
		b, err := os.ReadFile(filepath.Join("testdata", c.file))
		require.NoError(t, err)
		require.Equal(t, c.want, OfficialTitle(b, c.docType, c.number), c.file)
	}
}

func TestOfficialTitleShapes(t *testing.T) {
	page := func(paras ...string) []byte {
		s := "<html><head><title>x mới nhất</title></head><body><table><tr><td>QUỐC HỘI</td></tr></table>"
		for _, p := range paras {
			s += p
		}
		return []byte(s + `<p class="prov-article">Điều 1. Phạm vi</p><p>Chính phủ ban hành Nghị định khác trong thân.</p></body></html>`)
	}
	p := func(s string) string { return `<p class="prov-content">` + s + `</p>` }

	// Luật and Bộ luật: name first, then số hiệu (vbpl: "Bộ luật Hình sự số 100/2015/QH13")
	require.Equal(t, "Luật Đất đai số 31/2024/QH15",
		OfficialTitle(page(p("LUẬT"), p("ĐẤT ĐAI"), p("Quốc hội ban hành Luật Đất đai.")), "Luật", "31/2024/QH15"))
	require.Equal(t, "Bộ luật Lao động số 45/2019/QH14",
		OfficialTitle(page(p("BỘ LUẬT"), p("LAO ĐỘNG"), p("Quốc hội ban hành Bộ luật Lao động.")), "Bộ luật", "45/2019/QH14"))
	// a heading broken over two lines; old tone placement in the heading
	require.Equal(t, "Nghị định số 2/2026/NĐ-CP Quy định chi tiết về đơn giản hóa thủ tục của Bộ Tài chính",
		OfficialTitle(page(p("NGHỊ ĐỊNH"), p("QUY ĐỊNH CHI TIẾT VỀ ĐƠN GIẢN HOÁ"), p("THỦ TỤC CỦA BỘ TÀI CHÍNH"),
			p("Chính phủ ban hành Nghị định quy định chi tiết về đơn giản hóa thủ tục của Bộ Tài chính.")), "Nghị định", "2/2026/NĐ-CP"))
	// the đề nghị line and the clause in one sentence; the type spelled the clause's way
	require.Equal(t, "Thông tư số 3/2026/TT-BYT Quy định danh mục thuốc thiết yếu",
		OfficialTitle(page(p("THÔNG TƯ"), p("QUY ĐỊNH DANH MỤC THUỐC THIẾT YẾU"),
			p("Theo đề nghị của Cục trưởng Cục Quản lý Dược, Bộ trưởng Bộ Y tế ban hành Thông tư quy định danh mục thuốc thiết yếu.")),
			"thông tư", "3/2026/TT-BYT"))
	// entities, inline tags, a footnote mark
	require.Equal(t, "Thông tư số 2/2026/TT-BTC Hướng dẫn thuế & phí trước bạ",
		OfficialTitle(page(p("HƯỚNG DẪN THUẾ &amp; PHÍ TRƯỚC BẠ"),
			p("Bộ trưởng Bộ Tài chính ban hành Thông tư <b>hướng dẫn</b> thuế &amp; phí trước bạ.<sup>[1]</sup>")), "Thông tư", "2/2026/TT-BTC"))

	// the type in capitals in the clause
	require.Equal(t, "Nghị định số 5/2026/NĐ-CP Quy định về phí trước bạ",
		OfficialTitle(page(p("QUY ĐỊNH VỀ PHÍ TRƯỚC BẠ"), p("Chính phủ ban hành NGHỊ ĐỊNH quy định về phí trước bạ.")), "Nghị định", "5/2026/NĐ-CP"))

	// a Quyết định ends its preamble with "QUYẾT ĐỊNH:" — no clause, keep the old title
	require.Empty(t, OfficialTitle(page(p("QUYẾT ĐỊNH"), p("VỀ GIÁ ĐẤT"), p("Căn cứ Luật Tổ chức Chính phủ;"), p("QUYẾT ĐỊNH:")),
		"Quyết định", "1/2026/QĐ-TTg"))
	// the body's clauses do not count
	require.Empty(t, OfficialTitle(page(p("NGHỊ ĐỊNH")), "Nghị định", "1/2026/NĐ-CP"))
	// căn cứ lines name other documents
	require.Empty(t, OfficialTitle(page(p("NGHỊ ĐỊNH"), p("VỀ THỦ TỤC HÀNH CHÍNH"),
		p("Căn cứ Nghị định số 5/2020/NĐ-CP của Chính phủ ban hành Nghị định về thủ tục hành chính khác;")), "Nghị định", "1/2026/NĐ-CP"))
	// a clause that names nothing
	require.Empty(t, OfficialTitle(page(p("NGHỊ ĐỊNH"), p("Chính phủ ban hành Nghị định.")), "Nghị định", "1/2026/NĐ-CP"))
	// another type's clause does not count
	require.Empty(t, OfficialTitle(page(p("QUY ĐỊNH THỜI HẠN LƯU TRỮ HỒ SƠ"), p("Bộ trưởng ban hành Thông tư quy định thời hạn lưu trữ hồ sơ.")),
		"Nghị định", "1/2026/NĐ-CP"))
	// a longer type behind the metadata's ("Thông tư liên tịch" for "Thông tư")
	require.Empty(t, OfficialTitle(page(p("QUY ĐỊNH GIÁ DỊCH VỤ"), p("Các Bộ trưởng ban hành Thông tư liên tịch quy định giá dịch vụ.")),
		"Thông tư", "1/2026/TTLT-BTC-BYT"))
	// an ALL-CAPS heading that itself says "BAN HÀNH QUYẾT ĐỊNH" is no clause
	require.Empty(t, OfficialTitle(page(p("BÃI BỎ QUYẾT ĐỊNH SỐ 12/2019/QĐ-UBND BAN HÀNH QUYẾT ĐỊNH VỀ GIÁ ĐẤT TRÊN ĐỊA BÀN TỈNH")),
		"Quyết định", "5/2026/QĐ-UBND"))
	// "ban hành Nghị định số …", "… như sau:" do not agree with the heading
	require.Empty(t, OfficialTitle(page(p("QUY ĐỊNH VỀ PHÍ"), p("Chính phủ ban hành Nghị định số 5/2026/NĐ-CP quy định về phí.")),
		"Nghị định", "5/2026/NĐ-CP"))
	require.Empty(t, OfficialTitle(page(p("QUY ĐỊNH VỀ PHÍ"), p("Chính phủ ban hành Nghị định quy định về phí như sau:")),
		"Nghị định", "5/2026/NĐ-CP"))
	require.Empty(t, OfficialTitle(nil, "Nghị định", "1/2026/NĐ-CP"))
	require.Empty(t, OfficialTitle(page(p("QUY ĐỊNH CHI TIẾT"), p("Chính phủ ban hành Nghị định quy định chi tiết.")), "", "1/2026/NĐ-CP"))
}

// Documents without Điều (Chỉ thị, Nghị quyết, Kết luận) have their body
// scanned too: a body sentence that says "ban hành …" is never the title.
func TestOfficialTitleIgnoresBodySentences(t *testing.T) {
	doc := func(heading, body string) []byte {
		return []byte(`<html><body><p class="prov-content">CHỈ THỊ</p><p class="prov-content">` + heading + `</p>` +
			`<p>` + body + `</p><table><tr><td><p>Nơi nhận: Chính phủ ban hành Nghị định về phòng cháy rừng;</p></td></tr></table></body></html>`)
	}
	require.Empty(t, OfficialTitle(doc("VỀ TĂNG CƯỜNG PHÒNG CHÁY RỪNG",
		"Trong những năm qua, Thủ tướng Chính phủ đã ban hành Chỉ thị số 15/CT-TTg ngày 1 tháng 2 năm 2020 về phòng cháy, chữa cháy rừng."),
		"Chỉ thị", "12/CT-TTg"))
	require.Empty(t, OfficialTitle(doc("VỀ CƠ CHẾ ĐẶC THÙ",
		"1. Bộ Tài chính khẩn trương trình Chính phủ ban hành Nghị quyết về cơ chế đặc thù tháo gỡ khó khăn trước ngày 15 tháng 6."),
		"Nghị quyết", "77/NQ-CP"))
	require.Empty(t, OfficialTitle(doc("VỀ PHÒNG CHÁY RỪNG", "Không có câu ban hành."), "Nghị định", "1/2026/NĐ-CP"))
	// single-quoted body class still ends the preamble
	require.Empty(t, OfficialTitle([]byte(`<p>QUY ĐỊNH VỀ PHÍ</p><p class='prov-chapter'>Chương I</p>`+
		`<p>Chính phủ ban hành Nghị định quy định về phí.</p>`), "Nghị định", "5/2026/NĐ-CP"))
}
