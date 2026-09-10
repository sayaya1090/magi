package idebridge

import (
	"bytes"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/shortdir"
	"os"
)

func TestRelayDuplexAndClientEOF(t *testing.T) {
	dir, err := shortdir.Make("relay-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "d.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	input, write := io.Pipe()
	defer input.Close()
	defer write.Close()
	output, read := io.Pipe()
	defer output.Close()
	defer read.Close()
	done := make(chan int, 1)
	go func() { done <- relay(socket, input, read, io.Discard) }()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	request := []byte("{\"method\":\"transcript\"}\n")
	go write.Write(request)
	got := make([]byte, len(request))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, request) {
		t.Fatalf("request changed: %q", got)
	}
	frames := []byte("{\"ok\":true}\n{\"ok\":true,\"event\":{}}\n")
	go conn.Write(frames)
	got = make([]byte, len(frames))
	if _, err := io.ReadFull(output, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, frames) {
		t.Fatalf("frames changed: %q", got)
	}
	write.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not stop on stdin EOF")
	}
	if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("daemon connection survived EOF: %v", err)
	}
}
