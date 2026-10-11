package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestTLSIMAPSizeAndPartialFetch(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint(large), func(t *testing.T) {
			certServer := httptest.NewTLSServer(nil)
			cert := certServer.TLS.Certificates[0]
			roots := x509.NewCertPool()
			roots.AddCert(certServer.Certificate())
			certServer.Close()
			listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var mu sync.Mutex
			bodyCalls := 0
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				fmt.Fprint(conn, "* OK test IMAP\r\n")
				scan := bufio.NewScanner(conn)
				for scan.Scan() {
					line := scan.Text()
					fields := strings.Fields(line)
					if len(fields) < 2 {
						return
					}
					tag := fields[0]
					switch strings.ToUpper(fields[1]) {
					case "CAPABILITY":
						fmt.Fprint(conn, "* CAPABILITY IMAP4rev1 AUTH=PLAIN\r\n")
					case "LOGIN":
					case "EXAMINE":
						fmt.Fprint(conn, "* 1 EXISTS\r\n* OK [UIDVALIDITY 19] stable\r\n* FLAGS (\\Seen)\r\n")
					case "UID":
						if strings.Contains(line, "RFC822.SIZE") {
							size := 100
							if large {
								size = (2 << 20) + 2
							}
							fmt.Fprintf(conn, "* 1 FETCH (UID 7 RFC822.SIZE %d)\r\n", size)
						} else {
							mu.Lock()
							bodyCalls++
							mu.Unlock()
							if !strings.Contains(line, "BODY.PEEK[]<0.2097153>") {
								t.Errorf("unbounded fetch: %s", line)
							}
							raw := "Content-Type: text/plain\r\n\r\nTLS body"
							fmt.Fprintf(conn, "* 1 FETCH (UID 7 BODY[]<0> {%d}\r\n%s)\r\n", len(raw), raw)
						}
					case "LOGOUT":
						fmt.Fprintf(conn, "* BYE closing\r\n%s OK logout\r\n", tag)
						return
					default:
						t.Errorf("unexpected command %s", line)
					}
					fmt.Fprintf(conn, "%s OK complete\r\n", tag)
				}
			}()
			n := Network{Config: Config{IMAP: listener.Addr().String(), User: "test", Password: "test"}, tlsConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
			out, err := n.Read(context.Background(), "INBOX", 19, 7)
			if large {
				if err == nil || !strings.Contains(err.Error(), "2 MiB") {
					t.Fatalf("size error %v", err)
				}
			} else if err != nil || out.Text != "TLS body" {
				t.Fatalf("read: %v %v", out, err)
			}
			<-done
			mu.Lock()
			defer mu.Unlock()
			if large && bodyCalls != 0 {
				t.Fatal("downloaded oversize body")
			}
			if !large && bodyCalls != 1 {
				t.Fatal("missing partial fetch")
			}
		})
	}
}
