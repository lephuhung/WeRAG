package service

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"
)

func docxWithParts(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, xml := range parts {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(xml))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func docxWithBody(t *testing.T, body string) []byte {
	return docxWithParts(t, map[string]string{"word/document.xml": docxDocument(body)})
}

func docxDocument(body string) string {
	return `<?xml version="1.0"?><w:document xmlns:w="x"><w:body>` + body + `<w:sectPr/></w:body></w:document>`
}

func TestEmptyDocx(t *testing.T) {
	big := docxWithBody(t, `<w:p/>`+string(bytes.Repeat([]byte(`<w:p><w:pPr/></w:p>`), emptyDocxMaxPart/18)))
	for name, tc := range map[string]struct {
		data  []byte
		empty bool
	}{
		"no paragraph":     {docxWithBody(t, ""), true},
		"empty paragraphs": {docxWithBody(t, `<w:p/><w:p><w:r><w:t xml:space="preserve">  </w:t></w:r></w:p><w:p><w:r><w:t/></w:r></w:p>`), true},
		"word-saved empty": {docxWithBody(t, `<w:p><w:pPr><w:rPr><w:lang w:val="vi-VN"/></w:rPr></w:pPr><w:bookmarkStart w:id="0" w:name="_GoBack"/><w:bookmarkEnd w:id="0"/></w:p>`), true},
		"text":             {docxWithBody(t, `<w:p><w:r><w:t>Kính gửi</w:t></w:r></w:p>`), false},
		"text with attrs":  {docxWithBody(t, `<w:p><w:r><w:t xml:space="preserve"> Điều 1</w:t></w:r></w:p>`), false},
		"picture only":     {docxWithBody(t, `<w:p><w:r><w:drawing><wp:inline/></w:drawing></w:r></w:p>`), false},
		"text box":         {docxWithBody(t, `<w:p><w:r><mc:AlternateContent><mc:Choice/></mc:AlternateContent></w:r></w:p>`), false},
		"embedded html":    {docxWithBody(t, `<w:altChunk r:id="x"/>`), false},
		"math":             {docxWithBody(t, `<w:p><m:oMath><m:r><m:t>x+1</m:t></m:r></m:oMath></w:p>`), false},
		"other prefix":     {docxWithParts(t, map[string]string{"word/document.xml": `<ns0:document xmlns:ns0="x"><ns0:body/></ns0:document>`}), false},
		"header text":      {docxWithParts(t, map[string]string{"word/document.xml": docxDocument(""), "word/header1.xml": `<w:hdr><w:p><w:r><w:t>CÔNG AN TỈNH</w:t></w:r></w:p></w:hdr>`}), false},
		"part too big":     {big, false},
		"not a zip":        {[]byte("plain text"), false},
		"zip without body": {docxWithParts(t, map[string]string{"[Content_Types].xml": "<Types/>"}), false},
	} {
		if got := emptyDocx(tc.data); got != tc.empty {
			t.Errorf("%s: emptyDocx = %v, want %v", name, got, tc.empty)
		}
	}
}

// The blanks the products actually upload: "Tạo văn bản mới" of the web
// (frontend-next buildBlankDocument) and python-docx's default document.
func TestEmptyDocxRealBlanks(t *testing.T) {
	for _, name := range []string{"testdata/empty_web_blank.docx", "testdata/empty_python_docx.docx"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !emptyDocx(data) {
			t.Errorf("%s: a blank document must be empty", name)
		}
	}
}
