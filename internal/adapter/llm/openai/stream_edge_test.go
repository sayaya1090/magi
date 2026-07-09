package openai

import (
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A backend that returns 200 with an empty body (a model that says nothing) must
// end gracefully: no text, no finish, no error, no hang. Pins that an empty turn
// is a clean no-op rather than a stranded read or a spurious error.
func TestEmptyStreamGraceful(t *testing.T) {
	srv := sseServer(t, "")
	defer srv.Close()

	var text string
	var finishes, errs int
	for _, e := range collect(t, New(srv.URL, "")) {
		switch e.Type {
		case port.ProviderText:
			text += e.Text
		case port.ProviderFinish:
			finishes++
		case port.ProviderError:
			errs++
		}
	}
	if text != "" || finishes != 0 || errs != 0 {
		t.Errorf("empty stream should yield nothing; got text=%q finishes=%d errs=%d", text, finishes, errs)
	}
}

// A complete-but-empty completion (finish_reason + usage, zero content) is a valid
// answer: emit the finish and the metering, carry no text, raise no error. Pins
// that an empty answer is still metered (token accounting survives) and clean.
func TestEmptyCompletionWithUsage(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":0}}\n\n" +
		"data: [DONE]\n\n"
	srv := sseServer(t, body)
	defer srv.Close()

	var text string
	var finishes, usage, errs, inTok int
	for _, e := range collect(t, New(srv.URL, "")) {
		switch e.Type {
		case port.ProviderText:
			text += e.Text
		case port.ProviderFinish:
			finishes++
		case port.ProviderUsage:
			usage++
			if e.Usage != nil {
				inTok = e.Usage.In
			}
		case port.ProviderError:
			errs++
		}
	}
	if text != "" {
		t.Errorf("empty completion should carry no text, got %q", text)
	}
	if finishes != 1 || usage != 1 || errs != 0 {
		t.Errorf("want finish=1 usage=1 err=0; got finish=%d usage=%d err=%d", finishes, usage, errs)
	}
	if inTok != 7 {
		t.Errorf("usage In=%d want 7 (empty answers are still metered)", inTok)
	}
}
