package openai

import (
	"bufio"
	"bytes"
	"io"
)

// sseEvents scans an OpenAI-style SSE stream and invokes fn for each decoded `data:` payload.
// Malformed lines are skipped by the caller (fn decides); sseEvents only extracts the raw data
// payloads.
//
// done reports whether the stream DECLARED its end with `[DONE]`, as opposed to simply running
// out. Both used to come back as a nil error, so a connection cut at a clean line boundary was
// indistinguishable from a finished one and the partial answer was accepted in silence. The
// caller needs the two apart: an end nobody announced, with no finish_reason either, is a
// truncated turn.
func sseEvents(r io.Reader, fn func(data []byte) error) (done bool, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[len("data:"):])
		if bytes.Equal(data, []byte("[DONE]")) {
			return true, nil
		}
		// Copy because the scanner reuses its buffer.
		buf := make([]byte, len(data))
		copy(buf, data)
		if err := fn(buf); err != nil {
			return false, err
		}
	}
	return false, sc.Err()
}
