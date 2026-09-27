package office

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Office.js 에는 길이 없고 COM 에는 있는 기능을 **헬퍼가 직접 답하는 도구**로 낸다(tool.Local) — PDF 로 내보내기, Word 의
// 맞춤법 검사·문서 통계·문서 비교, Excel 의 목표값 찾기·스파크라인·슬라이서. 창의 손은 이 이름들을 모른다. 헬퍼가 창에게
// 「어느 문서인가」만 묻고(Word 는 MAGI.DOC 표식, Excel 은 통장 이름) 그 문서를 COM 으로 잡아 한다.
//
// 사용자 요청(2026-09-27): 「2021 에서 추가 연결 가능한 다른 툴은 없고?」. COM 은 Windows 에만 있으므로 다른 OS 에서는
// 목록에서 뺀다(hostcaps.go) — 부를 수 없는 도구를 광고하면 모델은 부르고 나서야 안다.

// comLocalOnThisOS — COM 은 Windows 에만 있다. 시험이 바꿔 끼운다.
var comLocalOnThisOS = runtime.GOOS == "windows"

// comLocalTools 는 앱마다 헬퍼가 COM 으로 답하는 도구.
var comLocalTools = map[string]map[string]bool{
	"word": {"export_pdf": true, "proofread": true, "document_stats": true, "compare_documents": true},
	"xl":   {"export_pdf": true, "goal_seek": true, "add_sparklines": true, "remove_sparklines": true, "add_slicer": true, "remove_slicer": true},
}

// comLocalTimeout 은 창에게 문서를 묻고 COM 일을 하는 데 쓰는 시간 — 큰 문서의 PDF·맞춤법 검사가 제일 오래 걸린다.
const comLocalTimeout = 2 * time.Minute

func comLocalContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), comLocalTimeout)
}

var unsafeFileChars = regexp.MustCompile(`[\\/:*?"<>|]+`)

// userDocumentsDir 는 저장 안 한 문서의 PDF 가 갈 자리 — 사람의 「문서」 폴더. 시험이 바꿔 끼운다.
var userDocumentsDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Documents"), nil
}

// pdfTarget 은 PDF 를 둘 자리를 정한다. 사람이 준 경로가 있으면 그것(전체 경로·.pdf·있는 폴더), 없으면 문서 파일 옆에
// 같은 이름으로, 저장 안 한 문서면 「문서」 폴더에 문서 이름으로. 있는 파일은 overwrite 없이 안 덮는다 — 조용히 덮으면
// 사람이 전에 보낸 PDF 가 사라진다.
func pdfTarget(saved, label, asked string, overwrite bool) (string, error) {
	path := strings.TrimSpace(asked)
	switch {
	case path != "":
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("path 는 전체 경로여야 합니다 — %q", path)
		}
	case saved != "":
		path = strings.TrimSuffix(saved, filepath.Ext(saved)) + ".pdf"
	default:
		dir, err := userDocumentsDir()
		if err != nil {
			return "", fmt.Errorf("저장 안 한 문서라 PDF 를 둘 자리를 정하지 못했습니다(%v) — path 에 .pdf 전체 경로를 주세요", err)
		}
		name := strings.TrimSpace(unsafeFileChars.ReplaceAllString(label, "_"))
		if name == "" {
			name = "문서"
		}
		path = filepath.Join(dir, name+".pdf")
	}
	if !strings.EqualFold(filepath.Ext(path), ".pdf") {
		return "", fmt.Errorf("path 는 .pdf 로 끝나야 합니다 — %q", path)
	}
	if st, err := os.Stat(filepath.Dir(path)); err != nil || !st.IsDir() {
		return "", fmt.Errorf("폴더가 없습니다 — %q", filepath.Dir(path))
	}
	if _, err := os.Stat(path); err == nil && !overwrite {
		return "", fmt.Errorf("이미 있는 파일입니다 — %q. 덮어쓰려면 overwrite: true", path)
	}
	return path, nil
}

// pdfDone 은 내보낸 파일을 확인해 답을 짓는다 — COM 이 「됐다」고 해도 파일이 없으면 된 게 아니다.
func pdfDone(path string, extra map[string]any) (map[string]any, []string, error) {
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return nil, nil, fmt.Errorf("PDF 를 내보냈다는데 파일이 없습니다 — %q", path)
	}
	out := map[string]any{"path": path, "bytes": st.Size()}
	for k, v := range extra {
		out[k] = v
	}
	return out, []string{fmt.Sprintf("PDF 로 내보냈습니다 — %s (%.1f KB)", path, float64(st.Size())/1024)}, nil
}

func comBool(args map[string]any, k string) bool {
	b, _ := args[k].(bool)
	return b
}
