package openai

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// severedSSEServer streams a partial SSE body inside a chunked response, then
// closes the socket WITHOUT the terminating zero-length chunk — a backend that
// dies mid-stream, before finish_reason. The client surfaces the truncated
// transfer as an unexpected EOF, which must reach consume as a real read error.
// It hijacks the connection because net/http would otherwise finalize the chunked
// stream cleanly on handler return (that clean case is covered separately).
func severedSSEServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("ResponseWriter is not a Hijacker")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		fmt.Fprint(buf, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
		fmt.Fprintf(buf, "%x\r\n%s\r\n", len(body), body) // one chunk carrying the partial body
		_ = buf.Flush()
		// Return here without writing the closing "0\r\n\r\n": the stream is cut
		// mid-transfer, so the client's body read fails with io.ErrUnexpectedEOF.
	}))
}

// A stream severed mid-flight, BEFORE finish_reason, must surface a ProviderError
// (not be mistaken for a clean end) while preserving the partial text already
// emitted — and it must NOT fabricate a finish or usage frame. This pins the
// `default` branch in consume and documents that a step severed before the usage
// chunk contributes no token accounting (no ProviderUsage is emitted).
func TestSeveredMidStreamSurfacesError(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"partial ans\"}}]}\n\n"
	srv := severedSSEServer(t, body)
	defer srv.Close()

	evs, ok := drain(t, New(srv.URL, ""), 2*time.Second)
	if !ok {
		t.Fatal("StreamChat hung on a severed stream")
	}
	var text string
	var errs, finishes, usage int
	for _, e := range evs {
		switch e.Type {
		case port.ProviderText:
			text += e.Text
		case port.ProviderError:
			errs++
		case port.ProviderFinish:
			finishes++
		case port.ProviderUsage:
			usage++
		}
	}
	if text != "partial ans" {
		t.Errorf("partial text not preserved across the sever: got %q", text)
	}
	if errs != 1 {
		t.Errorf("severed-before-finish stream must surface exactly one ProviderError; got %d", errs)
	}
	if finishes != 0 || usage != 0 {
		t.Errorf("a severed stream must not synthesize finish/usage; finishes=%d usage=%d", finishes, usage)
	}
}

// A stream cut at a CLEAN line boundary reaches EOF with no read error, so it used to be
// indistinguishable from a legitimate end: the partial answer was accepted SILENTLY — no
// ProviderError, no finish, no usage, just a shorter reply the caller had no way to question.
//
// It IS distinguishable, by what the stream never said. Measured against the backend this runs on
// before relying on it: `stop`, `length` and `tool_calls` responses all carry finish_reason, and
// all are followed by `[DONE]`. An end with neither is not a shape it produces — it is a cut.
//
// Scoped to a stream that delivered at least one frame: a body with no frames at all hands over no
// fragment that could be mistaken for a whole answer, and stays the clean no-op
// TestEmptyStreamGraceful pins.
func TestTruncationAtLineBoundaryIsReported(t *testing.T) {
	// A complete SSE frame with content but NO finish_reason, then a clean close.
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"half answer\"}}]}\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
		// Handler returns → net/http finalizes the chunked stream cleanly (EOF).
	}))
	defer srv.Close()

	evs, ok := drain(t, New(srv.URL, ""), 2*time.Second)
	if !ok {
		t.Fatal("StreamChat hung on a clean truncation")
	}
	var text, errText string
	var errs, finishes, usage int
	for _, e := range evs {
		switch e.Type {
		case port.ProviderText:
			text += e.Text
		case port.ProviderError:
			errs++
			if e.Err != nil {
				errText = e.Err.Error()
			}
		case port.ProviderFinish:
			finishes++
		case port.ProviderUsage:
			usage++
		}
	}
	// What arrived is still handed over — the fragment is evidence, not garbage.
	if text != "half answer" {
		t.Errorf("the partial text must still reach the caller, got %q", text)
	}
	// But it is no longer passed off as a finished turn.
	if errs != 1 {
		t.Errorf("a cut stream must say so exactly once, got %d errors", errs)
	}
	if !strings.Contains(errText, "cut off") || !strings.Contains(errText, "finish_reason") {
		t.Errorf("the error names what was missing and what it means: %q", errText)
	}
	if finishes != 0 || usage != 0 {
		t.Errorf("nothing may be synthesized for a turn that never finished; finishes=%d usage=%d", finishes, usage)
	}
}
