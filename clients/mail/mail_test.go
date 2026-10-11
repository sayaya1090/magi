package mail

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBackend struct {
	sent  int
	reads int
}

func (f *fakeBackend) List(context.Context, string) (uint32, []Item, error) {
	return 19, []Item{{UID: 7, Subject: "<script>alert(1)</script>"}}, nil
}
func (f *fakeBackend) Read(_ context.Context, _ string, v, u uint32) (Message, error) {
	f.reads++
	return Message{UID: u, UIDValidity: v, Raw: "original MIME", Text: "plain"}, nil
}
func (f *fakeBackend) Send(_ context.Context, d Draft) error {
	if err := validateDraft(d); err != nil {
		return err
	}
	f.sent++
	return nil
}
func TestToolsCannotSendAndRespectTarget(t *testing.T) {
	f := &fakeBackend{}
	a := NewApp(f, "user@example.com")
	target := a.Target("INBOX", 19, 7)
	d := json.RawMessage(`{"to":"a@example.com","subject":"Hello","body":"A draft"}`)
	out, err := a.Call(context.Background(), target, "mail_draft", d)
	if err != nil || out.(map[string]any)["sent"] != false || f.sent != 0 {
		t.Fatalf("draft sent: %v %v", out, err)
	}
	if _, err = a.Call(context.Background(), target, "mail_send", d); err == nil {
		t.Fatal("model gained sending tool")
	}
	args := json.RawMessage(`{"mailbox":"INBOX","uidValidity":19,"uid":7}`)
	if _, err = a.Call(context.Background(), "another-session", "mail_read", args); err == nil || f.reads != 0 {
		t.Fatal("cross-session read allowed")
	}
	if _, err = a.Call(context.Background(), target, "mail_read", args); err != nil || f.reads != 1 {
		t.Fatalf("selected read failed: %v", err)
	}
}
func TestReadDefaultsToBoundMessage(t *testing.T) {
	f := &fakeBackend{}
	a := NewApp(f, "me@example.test")
	out, err := a.Call(context.Background(), a.Target("Folder/Child", 19, 7), "mail_read", json.RawMessage(`{}`))
	if err != nil || out.(Message).UID != 7 || out.(Message).UIDValidity != 19 {
		t.Fatalf("default selected read: %v %v", out, err)
	}
	if _, err = a.Call(context.Background(), "another@example.test/INBOX/19/7", "mail_read", json.RawMessage(`{}`)); err == nil {
		t.Fatal("other account accepted")
	}
}

func TestSendRequiresExplicitConfirmation(t *testing.T) {
	f := &fakeBackend{}
	a := NewApp(f, "me")
	for _, confirm := range []string{"false", "true"} {
		r := httptest.NewRequest("POST", "/mail/send", strings.NewReader(`{"draft":{"to":"a@example.com","subject":"Hello","body":"Text"},"confirm":`+confirm+`}`))
		w := httptest.NewRecorder()
		a.HTTP(w, r)
		if confirm == "false" && (w.Code != 400 || f.sent != 0) {
			t.Fatal("sent without consent")
		}
		if confirm == "true" && (w.Code != 200 || f.sent != 1) {
			t.Fatalf("confirmed delivery: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestHeaderInjectionRejected(t *testing.T) {
	for _, d := range []Draft{{To: "a@example.com\r\nBcc: victim@example.com"}, {To: "a@example.com", Subject: "a\nBcc: victim@example.com"}} {
		if validateDraft(d) == nil {
			t.Fatal("header injection allowed")
		}
	}
}
func TestMIMEPreservesRawAndDecodesText(t *testing.T) {
	fixtures := []struct{ raw, want string }{{"Content-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\nSGVsbG8=", "Hello"}, {"Content-Type: text/plain\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\na=20b", "a b"}, {"Content-Type: multipart/alternative; boundary=b\r\n\r\n--b\r\nContent-Type: text/html\r\n\r\n<script>bad</script>\r\n--b\r\nContent-Type: text/plain\r\n\r\nsafe\r\n--b--\r\n", "safe"}}
	for _, f := range fixtures {
		text, err := plainMessage([]byte(f.raw))
		if err != nil || text != f.want {
			t.Fatalf("decoded %q expected %q: %v", text, f.want, err)
		}
	}
}
func TestDraftBodyAndSubjectLimits(t *testing.T) {
	if validateDraft(Draft{To: "bad"}) == nil {
		t.Fatal("bad address allowed")
	}
	if validateDraft(Draft{To: "a@example.com", Body: strings.Repeat("x", (1<<20)+1)}) == nil {
		t.Fatal("oversize body allowed")
	}
}
func TestUIRendersUntrustedMailAsText(t *testing.T) {
	b, err := Assets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "innerHTML") || !strings.Contains(s, "$('body').textContent=msg.text") || !strings.Contains(s, "$('raw').textContent=msg.raw") {
		t.Fatal("untrusted MIME must never become HTML")
	}
}

func TestMIMESkipsPlainTextAttachments(t *testing.T) {
	raw := "Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=note.txt\r\n\r\nattachment text\r\n--b\r\nContent-Type: text/plain\r\n\r\nactual body\r\n--b--\r\n"
	text, err := plainMessage([]byte(raw))
	if err != nil || text != "actual body" {
		t.Fatalf("attachment became body: %q %v", text, err)
	}
}
func TestConfigTLSDefaultAndRejectsPlaintext(t *testing.T) {
	t.Setenv("MAGI_MAIL_IMAP", "imap.example.test:993")
	t.Setenv("MAGI_MAIL_SMTP", "smtp.example.test:465")
	t.Setenv("MAGI_MAIL_USER", "me@example.test")
	t.Setenv("MAGI_MAIL_PASSWORD", "test-only")
	t.Setenv("MAGI_MAIL_FROM", "")
	t.Setenv("MAGI_MAIL_SMTP_TLS", "")
	c, err := FromEnv()
	if err != nil || c.SMTPMode != "implicit" || c.From != c.User {
		t.Fatalf("defaults %v %v", c, err)
	}
	t.Setenv("MAGI_MAIL_SMTP_TLS", "none")
	if _, err := FromEnv(); err == nil {
		t.Fatal("plaintext SMTP accepted")
	}
	t.Setenv("MAGI_MAIL_SMTP_TLS", "starttls")
	if _, err := FromEnv(); err != nil {
		t.Fatal(err)
	}
}
