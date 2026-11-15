package app

import (
	"strings"
	"testing"
)

// The system prompt and the visual-explanation skill carry the same rule as the mermaid tool:
// element details go in descriptions, and the prose does not explain each element.
func TestPromptAndSkillSendElementDetailsToDescriptions(t *testing.T) {
	a := New(nil, nil, nil, nil, nil, Config{ClientRole: "coding"})
	skill, _ := a.skillBody(t.TempDir(), "visual-explanation")
	for name, text := range map[string]string{"system prompt": outputFormatGuide, "visual-explanation": skill} {
		if !strings.Contains(text, "each element's details") {
			t.Errorf("%s does not send element details to descriptions", name)
		}
		for _, old := range []string{"together with a short explanation", "explain the important arrows"} {
			if strings.Contains(text, old) {
				t.Errorf("%s still says %q", name, old)
			}
		}
	}
}
