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
	if len(Terms("a 1 가")) != 0 {
		t.Fatal("noise terms retained")
	}
	if len(Terms("字")) != 1 {
		t.Fatal("single Han character lost")
	}
	if Scores("字", []string{"漢字文本"})[0] <= 0 {
		t.Fatal("single Han query cannot find character inside a longer run")
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
