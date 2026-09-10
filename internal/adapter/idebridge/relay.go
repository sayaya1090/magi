package idebridge

import (
	"fmt"
	"io"
	"net"
	"time"
)

// relay gives runtimes without Windows AF_UNIX support a byte stream through Go.
// EOF in either direction closes the socket; stdout contains only daemon frames.
func relay(socket string, stdin io.Reader, stdout, stderr io.Writer) int {
	c, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer c.Close()
	done := make(chan error, 2)
	go func() { _, e := io.Copy(c, stdin); done <- e }()
	go func() { _, e := io.Copy(stdout, c); done <- e }()
	if err := <-done; err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
