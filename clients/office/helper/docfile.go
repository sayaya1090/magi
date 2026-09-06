package office

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DocFile 은 사람 머신의 문서 파일 하나를 읽어 판에 실어 줄 모양. 그림(image.go)과 같은 길이다 —
// 모델은 경로만 말하고, 이 프로세스가 읽고, 내용을 보고 아니면 거절한다.
type DocFile struct {
	Path   string
	Name   string
	Base64 string
	Bytes  int
}

// maxDocBytes 는 받아 줄 문서 하나의 천장.
const maxDocBytes = 20 << 20 // 20MB

// docExts 는 Office.js 의 insertFileFromBase64(Word) · insertWorksheetsFromBase64(Excel) 가 받는 것.
var docExts = map[string]string{".docx": "Word", ".xlsx": "Excel"}

// ReadDocFile 은 경로 하나를 읽어 Office 문서(OOXML zip)인지 보고 넘겨준다. `want` 는 이 도구가 받는 확장자.
func ReadDocFile(path, want string) (DocFile, error) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return DocFile{}, fmt.Errorf("어느 파일인지 경로를 주세요")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		abs = raw
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return DocFile{}, fmt.Errorf("그런 파일이 없습니다: %s", abs)
		}
		return DocFile{}, fmt.Errorf("파일을 못 봤습니다(%s): %w", abs, err)
	}
	if st.IsDir() {
		return DocFile{}, fmt.Errorf("%s 는 폴더입니다 — 파일 하나를 짚어 주세요", abs)
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if ext != want {
		return DocFile{}, fmt.Errorf("%s 파일만 받습니다 — %s", want, abs)
	}
	if st.Size() > maxDocBytes {
		return DocFile{}, fmt.Errorf("파일이 너무 큽니다(%.1fMB, 최대 %dMB): %s", float64(st.Size())/(1<<20), maxDocBytes>>20, abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return DocFile{}, fmt.Errorf("파일을 못 읽었습니다(%s): %w", abs, err)
	}
	// **내용으로 가린다.** OOXML 은 zip 이다 — 이름만 .docx 인 다른 파일을 Office 에 밀어 넣지 않는다.
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return DocFile{}, fmt.Errorf("%s 는 %s 문서가 아닙니다(OOXML zip 이 아님)", abs, docExts[want])
	}
	return DocFile{Path: abs, Name: filepath.Base(abs), Base64: base64.StdEncoding.EncodeToString(data), Bytes: len(data)}, nil
}
