package office

import (
	"context"
	"fmt"
	"net/url"
)

// App 은 헬퍼 하나가 섬기는 Office 프로그램 하나 — 파워포인트·엑셀·워드. 세 헬퍼가 이름만 다른
// 복사본이었던 것을(2026-09-06 까지 셋) 여기 한 벌로 모으고, 다른 점은 전부 이 값에 둔다.
//
// **프로세스 하나·인증서 하나·포트 하나.** 사람이 신뢰 저장소에 넣는 인증서가 하나여야 하고,
// 자동 시작도 하나, 볼륨 판 Excel 의 신뢰 카탈로그 키도 하나다. 그래서 URL 은 `/{Key}/…` 로
// 갈라진다: `/word/taskpane.html`, `/word/mcp`, `/word/api/…`, `/word/hand/stream`.
type App struct {
	// Key 는 URL 접두·MCP 서버 이름(`mcp__word__…`)·`document` 키의 어원. 짧고 바뀌지 않는다.
	Key string
	// Product 는 사람이 부르는 프로그램 이름. 영어 오류문에 그대로 들어간다.
	Product string
	// Noun 은 그 프로그램의 문서를 부르는 영어 낱말(deck·workbook·document) — 모델이 읽는 거절문.
	Noun string
	// NounKo·PartKo 는 사람이 읽는 한국어(통합 문서·「시트는」 — 조사까지 붙여, 「는/은」을 고르지 않게).
	NounKo, PartKo string
	// DocPrefix 는 손이 붙을 때 헬퍼가 발급하는 문서 키의 앞머리(pid-·wb-·wd-).
	DocPrefix string
	// DocParam 은 작업창이 손 스트림에 자기 문서를 실어 보내는 쿼리 이름(presentation·workbook·doc).
	DocParam string
	// AddinDir 은 `clients/<AddinDir>/addin` — 작업창 소스가 있는 자리.
	AddinDir string
	// Workspace 는 설정 디렉토리 아래 컴패니언 워크스페이스 이름(powerpoint·excel·word).
	Workspace string
	// Skills 는 번들 스킬이 든 embed 경로(skills/word).
	Skills string
	// Catalogue 는 도구 표. DocumentProp 은 그 표의 모든 도구가 받는 `document` 인자.
	Catalogue    func(hasCouncil bool) []tool
	DocumentProp property
	// ValueEnums·EnumExempt·Refusal 은 인자 열거형 검사(enums.go). Refusal 이 nil 이면 enumRefusal.
	ValueEnums map[string][]string
	EnumExempt map[string]map[string]bool
	Refusal    func(app *App, toolName, where, key, got string) string
	// ArgExample 은 인자가 JSON 객체가 아닐 때 보여 주는 예. CheckArgs 는 그 앱만의 인자 규칙.
	ArgExample string
	CheckArgs  func(t tool, args map[string]any) error
	// WantsImage 는 이 호출이 헬퍼가 디스크에서 그림을 읽어 실어야 하는 것인가(mcp.go).
	WantsImage func(name string, args map[string]any) bool
	// WantsFile 은 이 호출이 디스크의 Office 문서를 읽어 실어야 하는 것인가 — 받는 확장자를 답한다("" 이면 아니다).
	WantsFile func(name string) string
	// Fallback 은 손이 거절한 호출을 헬퍼가 다른 길로 대신할 수 있는가(mcp.go) — 엑셀 2021 의 메모를 COM 노트로(xl_notes.go).
	// 두 번째 값이 false 면 이 길이 아니라 손의 오류가 그대로 간다.
	Fallback func(ctx context.Context, hand Hand, where, name string, args map[string]any, handErr string) (HandResult, bool, error)
	// Instructions 는 워크스페이스가 처음 생길 때 AGENTS.md 에 심는 운영 지침.
	Instructions string
	// MCPInstructions 는 initialize 응답의 instructions. RenderHint 는 그림을 못 보는 모델에게 대신 볼 것.
	MCPInstructions string
	RenderHint      string
	// Automation 은 모델이 손 대신 잡으려 드는 바깥 도구들 — 거절문이 이름을 대고 막는다.
	Automation string
}

// Base 는 이 앱의 URL 접두(`/word`).
func (a *App) Base() string { return "/" + a.Key }

// PageURL·MCPURL 은 매니페스트와 데몬이 두드리는 주소.
func (a *App) PageURL(port int) string { return Origin(port) + a.Base() + "/taskpane.html" }
func (a *App) MCPURL(port int, deck string) string {
	at := Origin(port) + a.Base() + "/mcp"
	if deck == "" {
		return at
	}
	return at + "?deck=" + url.QueryEscape(deck)
}

func (a *App) refusal(toolName, where, key, got string) string {
	if a.Refusal != nil {
		return a.Refusal(a, toolName, where, key, got)
	}
	return enumRefusal(a, toolName, where, key, got)
}

// PPT·XL·Word 가 셋이고 Apps 가 그 순서다. AppByKey 는 URL 첫 조각으로 고른다.
var (
	PPT = &App{
		Key: "ppt", Product: "PowerPoint", Noun: "deck", NounKo: "덱", PartKo: "슬라이드는",
		DocPrefix: "pid-", DocParam: "presentation", AddinDir: "powerpoint", Workspace: "powerpoint", Skills: "skills/powerpoint",
		Catalogue: pptCatalogue, DocumentProp: pptDocumentProp, ValueEnums: pptValueEnums, EnumExempt: nil,
		Refusal: func(app *App, toolName, where, key, got string) string {
			switch key {
			case "bullet_type", "bullet_style":
				return pptBulletRefusal(toolName, where, key, got)
			}
			return enumRefusal(app, toolName, where, key, got)
		},
		ArgExample: `{"slide": 3}`,
		CheckArgs: func(t tool, args map[string]any) error {
			if s, ok := args["slide"]; ok {
				if n, err := asInt(s); err != nil || n < 1 {
					return argError{fmt.Sprintf("%s: slide is a 1-based position, so it starts at 1 (got %v)", t.Name, s)}
				}
			}
			return nil
		},
		WantsImage: func(name string, args map[string]any) bool {
			return name == "add_image" || (name == "set_background" && fmt.Sprint(args["kind"]) == "picture")
		},
		Instructions: pptInstructions,
		MCPInstructions: "A deck is already open in PowerPoint and these tools are attached to it. " +
			"You do not create, open or upload a deck and there is no tool that does. Positions are " +
			"1-based. Charts, images, speaker notes, entrance animation and durable suggestions are " +
			"all supported; SmartArt is not, and nothing restyles an existing table.",
		RenderHint: "그림이 안 보이면 read_slide 의 수치(자리·크기·서체·색)로 판단하세요.",
		Automation: "PowerShell, COM automation or python-pptx",
	}
	XL = &App{
		Key: "xl", Product: "Excel", Noun: "workbook", NounKo: "통합 문서", PartKo: "시트는",
		DocPrefix: "wb-", DocParam: "workbook", AddinDir: "excel", Workspace: "excel", Skills: "skills/excel",
		Catalogue: xlCatalogue, DocumentProp: xlDocumentProp, ValueEnums: xlValueEnums, EnumExempt: xlEnumExempt,
		ArgExample: `{"address": "B2"}`,
		CheckArgs: func(t tool, args map[string]any) error {
			if s, ok := args["to"]; ok && t.Name == "move_sheet" {
				if n, err := asInt(s); err != nil || n < 1 {
					return argError{fmt.Sprintf("%s: to is a 1-based tab position, so it starts at 1 (got %v)", t.Name, s)}
				}
			}
			return nil
		},
		WantsImage: func(name string, _ map[string]any) bool { return name == "add_image" },
		Fallback:   xlNotesFallback,
		WantsFile: func(name string) string {
			switch name {
			case "insert_sheets_from_file":
				return ".xlsx"
			case "import_csv":
				return ".csv"
			}
			return ""
		},
		Instructions: xlInstructions,
		MCPInstructions: "A workbook is already open in Excel and these tools are attached to it. " +
			"You do not create, open or upload a workbook and there is no tool that does. Sheets are named " +
			"by their tab name; ranges are A1-style; omit sheet for the one the person is looking at. " +
			"Read list_sheets first.",
		RenderHint: "그림이 안 보이면 read_range 의 값과 describe_sheet 의 수치로 판단하세요.",
		Automation: "PowerShell, COM automation, openpyxl or pandas",
	}
	Word = &App{
		Key: "word", Product: "Word", Noun: "document", NounKo: "문서", PartKo: "문단은",
		DocPrefix: "wd-", DocParam: "doc", AddinDir: "word", Workspace: "word", Skills: "skills/word",
		Catalogue: wordCatalogue, DocumentProp: wordDocumentProp, ValueEnums: wordValueEnums, EnumExempt: wordEnumExempt,
		ArgExample: `{"paragraph": 3}`,
		WantsImage: func(name string, _ map[string]any) bool { return name == "insert_image" },
		WantsFile: func(name string) string {
			if name == "insert_file" {
				return ".docx"
			}
			return ""
		},
		Instructions: wordInstructions,
		MCPInstructions: "A document is already open in Word and these tools are attached to it. " +
			"You do not create, open or upload a document and there is no tool that does. Paragraphs are " +
			"addressed by 1-based number; from/to omitted means the whole body. Read list_paragraphs first; " +
			"read_html is how you look at formatting (Word cannot render a page image).",
		RenderHint: "그림이 안 보이면 read_paragraphs 의 글과 read_html 로 판단하세요.",
		Automation: "PowerShell, COM automation or python-docx",
	}
	Apps = []*App{PPT, XL, Word}
)

// AppByKey 는 `ppt`·`xl`·`word` 중 하나를 고른다. 모르는 이름은 nil.
func AppByKey(key string) *App {
	for _, a := range Apps {
		if a.Key == key {
			return a
		}
	}
	return nil
}
