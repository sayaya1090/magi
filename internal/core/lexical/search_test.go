package lexical

import (
	"reflect"
	"strings"
	"testing"
)

func TestUnicodeTermsAndNormalization(t *testing.T) {
	for _, pair := range [][2]string{{"설정창", "설정창"}, {"API", "ＡＰＩ"}} {
		if !reflect.DeepEqual(Terms(pair[0]), Terms(pair[1])) {
			t.Fatalf("normalization differs: %v", pair)
		}
	}
	terms := Terms("PPT설정창의 너비")
	for _, want := range []string{"ppt", "설정", "정창", "창의", "너비"} {
		found := false
		for _, got := range terms {
			found = found || got == want
		}
		if !found {
			t.Fatalf("missing %s in %v", want, terms)
		}
	}
	if len(Terms("a 1")) != 0 {
		t.Fatal("noise terms retained")
	}
	if len(Terms("字")) != 1 {
		t.Fatal("single Han character lost")
	}
	if len(Terms("락")) != 1 {
		t.Fatal("single Hangul character lost")
	}
	if Scores("字", []string{"漢字文本"})[0] <= 0 {
		t.Fatal("single Han query cannot find character inside a longer run")
	}
	if Scores("락", []string{"락이 걸렸다"})[0] <= 0 {
		t.Fatal("single Hangul query cannot find term inside text with josa")
	}
}

func TestBM25KoreanInflectionAndDistractors(t *testing.T) {
	docs := []string{"설정창 너비를 줄여도 드롭다운이 줄어들지 않았다", "슬라이드 노트 읽기의 토큰 사용량을 줄였다", "설정창의 " + strings.Repeat("설정창의 ", 100) + "다른 정보"}
	scores := Scores("설정창의 너비", docs)
	if scores[0] <= scores[1] || scores[0] <= scores[2] || scores[1] != 0 {
		t.Fatalf("ranking: %v", scores)
	}
	if !reflect.DeepEqual(scores, Scores("설정창의 너비", docs)) {
		t.Fatal("non-deterministic scores")
	}
	if Scores("없는주제", docs)[0] != 0 {
		t.Fatal("unrelated query matched")
	}
}

func TestSubTokenizationAndStopStemming(t *testing.T) {
	camelTerms := Terms("SearchManagedExperience")
	for _, want := range []string{"searchmanagedexperience", "search", "managed", "experience"} {
		found := false
		for _, got := range camelTerms {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in camel terms: %v", want, camelTerms)
		}
	}

	pathTerms := Terms("/Users/sayaya/.local/bin/magi")
	for _, want := range []string{"users", "sayaya", "local", "bin", "magi"} {
		found := false
		for _, got := range pathTerms {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in path terms: %v", want, pathTerms)
		}
	}

	codeTerms := Terms("engram_managed_test.go")
	for _, want := range []string{"engram", "managed", "test", "go"} {
		found := false
		for _, got := range codeTerms {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in code terms: %v", want, codeTerms)
		}
	}

	dockerTerms := Terms("docker server")
	for _, got := range dockerTerms {
		if got == "dock" || got == "serv" {
			t.Fatalf("technical keyword destructively stemmed: %s", got)
		}
	}
}

func TestFieldScoresBM25F(t *testing.T) {
	docs := []DocumentFields{
		{
			Fields: []Field{
				{Text: "port_rule", Weight: 3.0},
				{Text: "포트 충돌 해결 지침", Weight: 2.0},
				{Text: "lsof 명령어로 프로세스를 확인하고 종료한다", Weight: 1.0},
			},
		},
		{
			Fields: []Field{
				{Text: "git_reconcile", Weight: 3.0},
				{Text: "충돌 해결", Weight: 2.0},
				{Text: "git 머지 시 충돌이 발생하면 원장을 확인한다", Weight: 1.0},
			},
		},
	}

	scores := FieldScores("port_rule", docs)
	if scores[0] <= 0 || scores[0] <= scores[1] {
		t.Fatalf("expected doc 0 to win for port_rule: %v", scores)
	}

	scoresTitle := FieldScores("포트 충돌", docs)
	if scoresTitle[0] <= scoresTitle[1] {
		t.Fatalf("expected doc 0 to win for 포트 충돌: %v", scoresTitle)
	}
}
