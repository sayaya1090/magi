package office

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	x, _ := w.Create("word/document.xml")
	_, _ = x.Write([]byte("<w:document/>"))
	_ = w.Close()
	_ = f.Close()
}

// 문서 파일은 이름이 아니라 내용으로 받는다 — 그림과 같은 규칙.
func TestReadDocFileTakesOOXMLOnly(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.docx")
	writeZip(t, good)
	got, err := ReadDocFile(good, ".docx")
	if err != nil || got.Name != "a.docx" || got.Bytes == 0 || got.Base64 == "" {
		t.Fatalf("멀쩡한 docx 를 못 읽었다: %+v %v", got, err)
	}
	fake := filepath.Join(dir, "b.docx")
	_ = os.WriteFile(fake, []byte("not a zip"), 0o644)
	if _, err := ReadDocFile(fake, ".docx"); err == nil || !strings.Contains(err.Error(), "문서가 아닙니다") {
		t.Fatalf("zip 아닌 .docx 를 받았다: %v", err)
	}
	if _, err := ReadDocFile(good, ".xlsx"); err == nil || !strings.Contains(err.Error(), ".xlsx 파일만") {
		t.Fatalf("다른 확장자를 받았다: %v", err)
	}
	if _, err := ReadDocFile(filepath.Join(dir, "none.docx"), ".docx"); err == nil || !strings.Contains(err.Error(), "그런 파일이 없습니다") {
		t.Fatalf("없는 파일에 다른 말을 했다: %v", err)
	}
}
