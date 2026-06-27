package openai

import (
	"bufio"
	"bytes"
	"io"
)

// sseEvents scans an OpenAI-style SSE stream and invokes fn for each decoded
// `data:` payload. It stops at `[DONE]` or EOF. Malformed lines are skipped by
// the caller (fn decides); sseEvents only extracts the raw data payloads.
func sseEvents(r io.Reader, fn func(data []byte) error) error {
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
			return nil
		}
		// Copy because the scanner reuses its buffer.
		buf := make([]byte, len(data))
		copy(buf, data)
		if err := fn(buf); err != nil {
			return err
		}
	}
	return sc.Err()
}
