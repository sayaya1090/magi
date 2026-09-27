package office

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Word 의 COM 전용 도구 넷 — PDF 로 내보내기, 맞춤법·문법 검사 읽기, 문서 통계, 문서 비교. 창(Office.js)에는 길이 없다:
// 쪽 수도, 빨간 밑줄 목록도, 비교도 WordApi 에 없다(com_local.go 머리 주석).

// wordProof 는 Word 가 밑줄 친 자리 하나.
type wordProof struct {
	Kind        string   `json:"kind"` // spelling | grammar
	Text        string   `json:"text"`
	Paragraph   int      `json:"paragraph"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// wordStats 는 Word 의 「단어 개수」 창과 같은 값.
type wordStats struct {
	Pages, Words, Characters, CharactersWithSpaces, Paragraphs, Lines, EastAsian int
}

// wordExtra 는 COM 전용 도구가 닿는 구멍 — Windows 에서는 COM(word_extra_windows.go), 시험에서는 가짜.
type wordExtra interface {
	Paragraphs() (int, error)
	// FilePath 는 문서 파일의 전체 경로 — 저장 안 한 문서면 "".
	FilePath() (string, error)
	ExportPDF(path string) error
	// Proof 는 from–to 문단의 맞춤법·문법 표시를 limit 개까지. more 는 더 있었는가.
	Proof(from, to, limit int) (items []wordProof, more bool, err error)
	Stats() (wordStats, error)
	// Compare 는 이 문서와 other 파일을 비교해 새 문서를 연다. otherIsRevised 면 other 가 고친 판이다.
	Compare(other string, otherIsRevised bool) (resultName string, revisions int, err error)
	Close()
}

var openWordExtra = openWordExtraOS

// wordLocal 은 헬퍼가 답하는 Word 도구 하나 — 창에게 문서 표식만 묻고 COM 으로 한다.
func wordLocal(name string) func(Hand, string, map[string]any) (HandResult, error) {
	return func(hand Hand, where string, args map[string]any) (HandResult, error) {
		if !comLocalOnThisOS {
			return HandResult{}, fmt.Errorf("%s 은 Windows 의 Word 에서만 됩니다(COM) — 이 컴퓨터에는 그 길이 없습니다", name)
		}
		ctx, cancel := comLocalContext()
		defer cancel()
		probe, err := hand.Call(ctx, where, "list_paragraphs", map[string]any{"from": 1, "to": 1})
		if err != nil {
			return HandResult{}, fmt.Errorf("%s: 어느 문서인지 창이 답하지 못했습니다(%v)", name, err)
		}
		docKey := probe.Document
		if docKey == "" {
			docKey = where
		}
		id := strings.TrimPrefix(docKey, "wd-")
		if id == "" || id == docKey {
			return HandResult{}, fmt.Errorf("%s: 문서 표식(MAGI.DOC)을 모릅니다(키 %q) — 작업창을 다시 열면 새겨집니다", name, docKey)
		}
		x, err := openWordExtra(id)
		if err != nil {
			return HandResult{}, fmt.Errorf("%s: %v", name, err)
		}
		defer x.Close()
		res, changed, err := wordExtraRun(x, probe.Label, name, args)
		if err != nil {
			return HandResult{}, fmt.Errorf("%s: %v", name, err)
		}
		res["via"] = wordComVia
		return HandResult{Document: docKey, Label: probe.Label, Result: res, Changed: changed}, nil
	}
}

func wordExtraRun(x wordExtra, label, name string, args map[string]any) (map[string]any, []string, error) {
	switch name {
	case "export_pdf":
		saved, err := x.FilePath()
		if err != nil {
			return nil, nil, err
		}
		path, err := pdfTarget(saved, label, wcStr(args, "path"), comBool(args, "overwrite"))
		if err != nil {
			return nil, nil, err
		}
		if err := x.ExportPDF(path); err != nil {
			return nil, nil, err
		}
		return pdfDone(path, nil)

	case "proofread":
		total, err := x.Paragraphs()
		if err != nil {
			return nil, nil, err
		}
		// 범위는 다른 Word 도구와 같은 규칙이다(fromProp·toProp): 둘 다 없으면 본문 전체, to 가 없으면 from 하나.
		from, to := wcInt(args, "from"), wcInt(args, "to")
		switch {
		case from == 0 && to == 0:
			from, to = 1, total
		case to == 0:
			to = from
		case from == 0:
			from = to
		}
		if from < 1 || to < from || to > total {
			return nil, nil, fmt.Errorf("문서에 문단 %d–%d 이 없습니다 — 문단 %d개", from, to, total)
		}
		limit := wcInt(args, "limit")
		if limit <= 0 {
			limit = 50
		}
		if limit > 200 {
			limit = 200
		}
		items, more, err := x.Proof(from, to, limit)
		if err != nil {
			return nil, nil, err
		}
		if items == nil {
			items = []wordProof{}
		}
		spell, gram := 0, 0
		for _, it := range items {
			if it.Kind == "grammar" {
				gram++
			} else {
				spell++
			}
		}
		said := fmt.Sprintf("문단 %d–%d: 맞춤법 %d · 문법 %d", from, to, spell, gram)
		if more {
			said += fmt.Sprintf(" — %d개까지만 실었습니다(더 있음, limit 이나 from/to 로 나눠 읽으세요)", limit)
		}
		lines := []string{said}
		if len(items) == 0 {
			lines = append(lines, "Word 가 표시한 것이 없습니다 — 그 언어의 교정 도구가 안 깔렸으면 틀린 곳이 있어도 비어 보입니다")
		}
		return map[string]any{"from": from, "to": to, "count": len(items), "more": more, "items": items}, lines, nil

	case "document_stats":
		st, err := x.Stats()
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{
				"pages": st.Pages, "words": st.Words, "characters": st.Characters, "characters_with_spaces": st.CharactersWithSpaces,
				"paragraphs": st.Paragraphs, "lines": st.Lines, "east_asian_characters": st.EastAsian,
			}, []string{fmt.Sprintf("%d쪽 · 단어 %d · 글자 %d(공백 포함 %d) · 문단 %d · 줄 %d", st.Pages, st.Words, st.Characters,
				st.CharactersWithSpaces, st.Paragraphs, st.Lines)}, nil

	case "compare_documents":
		other := strings.TrimSpace(wcStr(args, "path"))
		if other == "" {
			return nil, nil, fmt.Errorf("path 가 없습니다 — 비교할 Word 파일(.docx)의 전체 경로")
		}
		if !filepath.IsAbs(other) {
			return nil, nil, fmt.Errorf("path 는 전체 경로여야 합니다 — %q", other)
		}
		switch strings.ToLower(filepath.Ext(other)) {
		case ".docx", ".doc", ".docm", ".dotx", ".rtf":
		default:
			return nil, nil, fmt.Errorf("Word 문서가 아닙니다 — %q (.docx·.doc·.docm·.rtf)", other)
		}
		if st, err := os.Stat(other); err != nil || st.IsDir() {
			return nil, nil, fmt.Errorf("파일이 없습니다 — %q", other)
		}
		if saved, _ := x.FilePath(); saved != "" && strings.EqualFold(filepath.Clean(saved), filepath.Clean(other)) {
			return nil, nil, fmt.Errorf("이 문서 자신과는 비교할 수 없습니다 — 다른 판의 파일을 주세요")
		}
		as := strings.ToLower(strings.TrimSpace(wcStr(args, "as")))
		if as == "" {
			as = "revised"
		}
		if as != "revised" && as != "original" {
			return nil, nil, fmt.Errorf("as 는 revised(그 파일이 고친 판) 또는 original(그 파일이 원본) — %q", as)
		}
		name, n, err := x.Compare(other, as == "revised")
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{"result_document": name, "revisions": n, "other": other, "other_is": as},
			[]string{fmt.Sprintf("비교 결과를 새 문서 「%s」 로 열었습니다 — 변경 %d건(변경 추적으로 표시). 이 문서와 %s 는 그대로입니다", name, n, filepath.Base(other)),
				"비교 결과 문서에는 magi 작업창이 안 붙어 있습니다 — 그 문서를 고치려면 거기서 작업창을 여세요"}, nil
	}
	return nil, nil, fmt.Errorf("모르는 도구 %s", name)
}

// wordComLocalTools 는 Word 카탈로그에 붙는 COM 전용 도구들 — Windows 가 아니면 hostcaps 가 목록에서 뺀다.
func wordComLocalTools(declare string) []tool {
	return []tool{
		{
			Name: "export_pdf",
			Desc: "Save this document as a PDF file (the document itself is untouched). Without path it goes next to the " +
				"document with the same name, or into the person's Documents folder if it was never saved. Refuses to " +
				"overwrite an existing file unless overwrite is true. Windows desktop Word only." + declare,
			Props: []property{
				{Name: "path", Type: "string", Desc: "Full path ending in .pdf."},
				{Name: "overwrite", Type: "boolean", Desc: "Replace an existing file."},
			},
			Local: wordLocal("export_pdf"),
		},
		{
			Name: "proofread",
			Desc: "What Word itself has underlined — spelling (red) and grammar (blue) — with the paragraph number and, for " +
				"spelling, Word's suggestions. The way to 「맞춤법 봐 줘」 without guessing: fix with replace_all or " +
				"replace_paragraph afterwards. Empty when the language's proofing tools are not installed. Windows desktop Word only." + declare,
			Props: []property{
				fromProp,
				toProp,
				{Name: "limit", Type: "integer", Desc: "Most items to return (default 50, at most 200)."},
			},
			ReadOnly: true,
			Local:    wordLocal("proofread"),
		},
		{
			Name: "document_stats",
			Desc: "Page, word, character, paragraph and line counts as Word's own Word Count shows them — pages are not " +
				"knowable any other way. Windows desktop Word only." + declare,
			Props:    []property{},
			ReadOnly: true,
			Local:    wordLocal("document_stats"),
		},
		{
			Name: "compare_documents",
			Desc: "Compare this document with another Word file and open the result as a NEW document with the differences " +
				"shown as tracked changes (Word's Compare). Neither input is changed. By default the other file is the " +
				"revised version; as: \"original\" if it is the older one. The result has no magi pane attached. Windows desktop Word only." + declare,
			Props: []property{
				{Name: "path", Type: "string", Desc: "Full path of the other .docx. Required."},
				{Name: "as", Type: "string", Desc: "revised (default) or original — what the other file is.", Enum: []string{"revised", "original"}},
			},
			Required: []string{"path"},
			Local:    wordLocal("compare_documents"),
		},
	}
}
