package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// Statements of fact stay; a sentence that addresses or orders the agent is dropped.
func TestExcerptStatementKeepsFactsAndDropsOrders(t *testing.T) {
	cases := []struct{ in, want string }{
		{"The invoice job retries twice. Always check the log first. It runs at 02:00.", "The invoice job retries twice. It runs at 02:00."},
		{"You should restart the daemon. The socket path is /tmp/x.", "The socket path is /tmp/x."},
		{"셀렉터가 상세 화면을 빠뜨렸다. 두 규칙에 셀렉터를 추가할 것. 값은 24px이다.", "셀렉터가 상세 화면을 빠뜨렸다. 값은 24px이다."},
		{"배포 전에 테스트를 돌리세요. 이 단계는 CI가 한다.", "이 단계는 CI가 한다."},
		{"Do not push. Never force.", ""},
	}
	for _, c := range cases {
		if got := excerptStatement(c.in); got != c.want {
			t.Errorf("%q\n got  %q\n want %q", c.in, got, c.want)
		}
	}
}

// An excerpt that looks like it carries a secret is dropped whole.
func TestExcerptStatementDropsSecrets(t *testing.T) {
	for _, s := range []string{
		"The token was ghp_" + strings.Repeat("a", 36) + " in the env.",
		"AWS key AKIAABCDEFGHIJKLMNOP was rotated.",
		"The config had password=hunter22 in it.",
		"-----BEGIN OPENSSH PRIVATE KEY----- blob",
	} {
		if got := excerptStatement(s); got != "" {
			t.Errorf("secret-looking excerpt kept: %q -> %q", s, got)
		}
	}
}

// The prompt list carries only filtered passages, and a memory whose passage is all orders is not listed.
func TestExperienceExcerptsAreFiltered(t *testing.T) {
	got := experienceExcerpts([]port.Memory{
		{ID: "a", Excerpt: "The cache lives in /tmp/x. Remember to clear it."},
		{ID: "b", Excerpt: "Never run it twice."},
	})
	want := "Matching passages (recall_memory returns the full text):\n- a: The cache lives in /tmp/x."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
