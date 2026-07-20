package builtin

import (
	"encoding/json"
	"testing"
)

// flexTasks must absorb the shapes weak models actually emit for the `tasks`
// fan-out, so a delegation call is never rejected over the field's SHAPE. The
// double-encoded-string case is the one observed live (fix-ocaml-gc bench): a
// model stuck in a search loop tried to escape via fan-out but sent tasks as a
// JSON string, the strict []subTask rejected it, and the escape never happened.
func TestFlexTasksShapes(t *testing.T) {
	want := []subTask{{Agent: "locator", Prompt: "find X"}, {Agent: "explorer", Prompt: "map Y"}}
	eq := func(got flexTasks, exp []subTask) bool {
		if len(got) != len(exp) {
			return false
		}
		for i := range exp {
			if got[i].Agent != exp[i].Agent || got[i].Prompt != exp[i].Prompt {
				return false
			}
		}
		return true
	}

	cases := []struct {
		name string
		raw  string
		want []subTask
	}{
		{"array", `{"tasks":[{"agent":"locator","prompt":"find X"},{"agent":"explorer","prompt":"map Y"}]}`, want},
		// The live failure: value is a JSON *string* whose content is the array.
		{"double-encoded string", `{"tasks":"[{\"agent\":\"locator\",\"prompt\":\"find X\"},{\"agent\":\"explorer\",\"prompt\":\"map Y\"}]"}`, want},
		{"single object", `{"tasks":{"agent":"locator","prompt":"find X"}}`, want[:1]},
		{"string-wrapped single object", `{"tasks":"{\"agent\":\"locator\",\"prompt\":\"find X\"}"}`, want[:1]},
		{"null", `{"tasks":null}`, nil},
		{"empty string", `{"tasks":""}`, nil},
		{"absent", `{"agent":"locator","prompt":"solo"}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var a taskArgs
			if err := json.Unmarshal([]byte(c.raw), &a); err != nil {
				t.Fatalf("unmarshal must not fail on a tolerated shape: %v", err)
			}
			if !eq(a.Tasks, c.want) {
				t.Errorf("tasks = %+v, want %+v", a.Tasks, c.want)
			}
		})
	}
}
