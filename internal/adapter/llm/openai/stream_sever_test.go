package openai

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

// Characterization (known gap): when a stream is cut at a CLEAN line boundary
// before finish_reason, the scanner reaches EOF with no error, so consume cannot
// distinguish it from a legitimate end. The partial answer is therefore accepted
// SILENTLY — no ProviderError, no finish, no usage. This test pins that current
// behavior so a future change that detects a finish-less EOF is a conscious one.
func TestSilentTruncationAtLineBoundary(t *testing.T) {
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
	if text != "half answer" {
		t.Errorf("partial text = %q, want %q", text, "half answer")
	}
	// Current behavior: a clean-boundary truncation is indistinguishable from a
	// normal end, so nothing is flagged. If any of these ever becomes non-zero,
	// the finish-less-EOF handling changed — update this test deliberately.
	if errs != 0 {
		t.Errorf("clean-boundary truncation currently surfaces no error; got %d (behavior changed?)", errs)
	}
	if finishes != 0 || usage != 0 {
		t.Errorf("clean-boundary truncation emits no finish/usage; finishes=%d usage=%d", finishes, usage)
	}
}
