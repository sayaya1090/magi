package idebridge

import (
	"fmt"
	"io"
	"net"
	"time"
)

// relay gives runtimes without Windows AF_UNIX support a byte stream through Go.
//
// ⚠ **stdin ending is the caller finishing its side, not the conversation ending.** This used to
// return as soon as EITHER copy finished, and the deferred Close then tore the socket down — so a
// caller that wrote its request and closed stdin lost the answer. Measured 2026-09-11:
//
//	echo '{"method":"about"}' | magi ide-bridge --raw-socket <sock>
//	→ stdout empty, exit 0
//
// A direct connection does not behave that way. A client that half-closes (`CloseWrite`) still
// reads the reply — measured on the same socket in the same run — and this relay exists to BE that
// connection for a runtime that cannot open one. Being strictly weaker than the thing it stands in
// for is the defect: the caller is not doing anything wrong, and the failure is silent, with a
// zero exit and an empty stream.
//
// So stdin ending closes the socket's WRITE half and nothing else. What ends the relay is the
// daemon closing — which is how a stream door says there is no more coming — or an error on either
// side. The read copy is the one whose end is the end.
func relay(socket string, stdin io.Reader, stdout, stderr io.Writer) int {
	c, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer c.Close()

	// The caller's side. Its EOF is reported as a half-close so the daemon sees the request stream
	// end — a door that reads to EOF gets what it is waiting for — while the answer stream stays
	// open. A write error is the caller's pipe going away, which ends the relay.
	sendErr := make(chan error, 1)
	go func() {
		_, e := io.Copy(c, stdin)
		if e == nil {
			if uc, ok := c.(*net.UnixConn); ok {
				e = uc.CloseWrite()
			}
		}
		sendErr <- e
	}()

	_, rerr := io.Copy(stdout, c)
	if rerr != nil {
		fmt.Fprintln(stderr, rerr)
		return 1
	}
	// The daemon has closed. A send that failed on the way is still worth saying — it means the
	// request was not wholly delivered, and an answer built from half of one is not an answer.
	select {
	case e := <-sendErr:
		if e != nil {
			fmt.Fprintln(stderr, e)
			return 1
		}
	default:
	}
	return 0
}
