package office

import (
	"encoding/json"
	"testing"
)

// 시트·슬라이드·문단 칸은 그 호출의 주제라고 스키마에 선언한다(`x-magi-topic`). 코어는 그 값으로 접힌 대화를 샤드로
// 나눠 `recall_context` 가 되찾는다 — 선언이 없으면 Office 대화는 통째로 「discussion」 한 조각이다.
func TestTopicArgumentsAreDeclaredInTheSchema(t *testing.T) {
	want := map[string]string{"ppt": "slide", "xl": "sheet", "word": "paragraph"}
	for _, app := range Apps {
		declared := 0
		for _, tl := range app.Catalogue(false) {
			var sc struct {
				Properties map[string]map[string]any `json:"properties"`
			}
			if err := json.Unmarshal(schemaOf(app, tl), &sc); err != nil {
				t.Fatalf("%s %s: %v", app.Key, tl.Name, err)
			}
			for name, prop := range sc.Properties {
				topic, _ := prop["x-magi-topic"].(bool)
				if name == want[app.Key] && !topic {
					t.Errorf("%s %s: %s 칸이 주제로 선언돼 있지 않다", app.Key, tl.Name, name)
				}
				if topic {
					declared++
				}
			}
		}
		if declared == 0 {
			t.Errorf("%s: 주제 칸을 하나도 선언하지 않았다", app.Key)
		}
	}
}
