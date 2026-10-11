// Package mail implements a local IMAP/SMTP client. Model tools never send mail.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	imap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type Config struct{ IMAP, SMTP, User, Password, From, SMTPMode string }

func FromEnv() (Config, error) {
	c := Config{IMAP: os.Getenv("MAGI_MAIL_IMAP"), SMTP: os.Getenv("MAGI_MAIL_SMTP"), User: os.Getenv("MAGI_MAIL_USER"), Password: os.Getenv("MAGI_MAIL_PASSWORD"), From: os.Getenv("MAGI_MAIL_FROM"), SMTPMode: os.Getenv("MAGI_MAIL_SMTP_TLS")}
	if c.SMTPMode == "" {
		c.SMTPMode = "implicit"
	}
	if c.SMTPMode != "implicit" && c.SMTPMode != "starttls" {
		return c, fmt.Errorf("MAGI_MAIL_SMTP_TLS must be implicit or starttls")
	}
	if c.From == "" {
		c.From = c.User
	}
	if c.IMAP == "" || c.SMTP == "" || c.User == "" || c.Password == "" {
		return c, fmt.Errorf("set MAGI_MAIL_IMAP, MAGI_MAIL_SMTP, MAGI_MAIL_USER and MAGI_MAIL_PASSWORD")
	}
	return c, nil
}

type Item struct {
	UID     uint32    `json:"uid"`
	Subject string    `json:"subject"`
	From    string    `json:"from"`
	Date    time.Time `json:"date"`
}
type Message struct {
	UID         uint32 `json:"uid"`
	UIDValidity uint32 `json:"uidValidity"`
	Raw         string `json:"raw"`
	Text        string `json:"text"`
}
type Draft struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}
type Backend interface {
	List(context.Context, string) (uint32, []Item, error)
	Read(context.Context, string, uint32, uint32) (Message, error)
	Send(context.Context, Draft) error
}
type Network struct {
	Config    Config
	tlsConfig *tls.Config
}

func (n Network) connect(ctx context.Context) (*client.Client, error) {
	host, _, err := net.SplitHostPort(n.Config.IMAP)
	if err != nil {
		return nil, fmt.Errorf("IMAP address must be host:port")
	}
	conn, err := (&tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: n.tlsFor(host)}).DialContext(ctx, "tcp", n.Config.IMAP)
	if err != nil {
		return nil, fmt.Errorf("IMAP TLS connection failed")
	}
	if err = conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("IMAP deadline setup failed")
	}
	c, err := client.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("IMAP greeting failed")
	}
	if err = c.Login(n.Config.User, n.Config.Password); err != nil {
		c.Logout()
		return nil, fmt.Errorf("IMAP authentication failed")
	}
	return c, nil
}
func (n Network) List(ctx context.Context, box string) (uint32, []Item, error) {
	c, err := n.connect(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer c.Logout()
	s, err := c.Select(box, true)
	if err != nil {
		return 0, nil, fmt.Errorf("mailbox unavailable")
	}
	out := []Item{}
	if s.Messages == 0 {
		return s.UidValidity, out, nil
	}
	set := new(imap.SeqSet)
	start := uint32(1)
	if s.Messages > 50 {
		start = s.Messages - 49
	}
	set.AddRange(start, s.Messages)
	ch := make(chan *imap.Message, 50)
	err = c.Fetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope}, ch)
	if err != nil {
		return 0, nil, fmt.Errorf("mail listing failed")
	}
	for m := range ch {
		if m.Envelope == nil {
			continue
		}
		from := ""
		if len(m.Envelope.From) > 0 {
			from = m.Envelope.From[0].Address()
		}
		out = append(out, Item{m.Uid, m.Envelope.Subject, from, m.Envelope.Date})
	}
	return s.UidValidity, out, nil
}
func (n Network) Read(ctx context.Context, box string, valid, uid uint32) (Message, error) {
	c, err := n.connect(ctx)
	if err != nil {
		return Message{}, err
	}
	defer c.Logout()
	s, err := c.Select(box, true)
	if err != nil || s.UidValidity != valid {
		return Message{}, fmt.Errorf("mailbox identity changed; refresh the list")
	}
	set := new(imap.SeqSet)
	set.AddNum(uid)
	sizeCh := make(chan *imap.Message, 1)
	if err = c.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchRFC822Size}, sizeCh); err != nil {
		return Message{}, fmt.Errorf("mail size lookup failed")
	}
	meta := <-sizeCh
	if meta == nil {
		return Message{}, fmt.Errorf("message no longer exists")
	}
	if meta.Size > 2<<20 {
		return Message{}, fmt.Errorf("message exceeds 2 MiB limit")
	}
	section := &imap.BodySectionName{Peek: true, Partial: []int{0, (2 << 20) + 1}}
	ch := make(chan *imap.Message, 1)
	err = c.UidFetch(set, []imap.FetchItem{imap.FetchUid, section.FetchItem()}, ch)
	if err != nil {
		return Message{}, fmt.Errorf("mail read failed")
	}
	m := <-ch
	if m == nil {
		return Message{}, fmt.Errorf("message no longer exists")
	}
	r := m.GetBody(section)
	if r == nil {
		return Message{}, fmt.Errorf("message body unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(r, 2<<20+1))
	if err != nil || len(raw) > 2<<20 {
		return Message{}, fmt.Errorf("message exceeds 2 MiB limit")
	}
	text, err := plainMessage(raw)
	if err != nil {
		return Message{}, err
	}
	return Message{uid, valid, string(raw), text}, nil
}
func plainMessage(raw []byte) (string, error) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("invalid MIME message")
	}
	text, err := plainPart(m.Header.Get("Content-Type"), decodeTransfer(m.Header.Get("Content-Transfer-Encoding"), m.Body))
	if err == nil && text == "" {
		text = "[No plain text body; raw MIME is available]"
	}
	return text, err
}
func plainPart(contentType string, r io.Reader) (string, error) {
	t, p, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(t, "multipart/") {
		mr := multipart.NewReader(r, p["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("invalid MIME multipart")
			}
			if disposition, _, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition")); disposition == "attachment" {
				continue
			}
			text, err := plainPart(part.Header.Get("Content-Type"), decodeTransfer(part.Header.Get("Content-Transfer-Encoding"), part))
			if err != nil {
				return "", err
			}
			if text != "" {
				return text, nil
			}
		}
		return "", nil
	}
	if t == "" || t == "text/plain" {
		b, err := io.ReadAll(r)
		return string(b), err
	}
	return "", nil
}
func validateDraft(d Draft) error {
	if strings.ContainsAny(d.To+d.Subject, "\r\n") {
		return fmt.Errorf("recipient and subject must be single lines")
	}
	if _, err := mail.ParseAddress(d.To); err != nil {
		return fmt.Errorf("enter a single valid recipient address")
	}
	if len(d.Body) > 1<<20 {
		return fmt.Errorf("draft exceeds 1 MiB")
	}
	return nil
}
func (n Network) Send(ctx context.Context, d Draft) error {
	if err := validateDraft(d); err != nil {
		return err
	}
	from, err := mail.ParseAddress(n.Config.From)
	if err != nil {
		return fmt.Errorf("invalid sender address")
	}
	to, _ := mail.ParseAddress(d.To)
	host, _, err := net.SplitHostPort(n.Config.SMTP)
	if err != nil {
		return fmt.Errorf("SMTP address must be host:port")
	}
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	if n.Config.SMTPMode == "starttls" {
		conn, err = (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", n.Config.SMTP)
	} else {
		conn, err = (&tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: tlsConfig}).DialContext(ctx, "tcp", n.Config.SMTP)
	}
	if err != nil {
		return fmt.Errorf("SMTP connection failed")
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return fmt.Errorf("SMTP deadline setup failed")
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("SMTP greeting failed")
	}
	defer c.Close()
	if n.Config.SMTPMode == "starttls" {
		if err = c.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("SMTP server does not support secure STARTTLS")
		}
	}
	if err = c.Auth(smtp.PlainAuth("", n.Config.User, n.Config.Password, host)); err != nil {
		return fmt.Errorf("SMTP authentication failed")
	}
	if err = c.Mail(from.Address); err != nil {
		return fmt.Errorf("SMTP sender rejected")
	}
	if err = c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("SMTP recipient rejected")
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP data unavailable")
	}
	body := strings.ReplaceAll(strings.ReplaceAll(d.Body, "\r\n", "\n"), "\n", "\r\n")
	_, err = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", from.String(), to.String(), mime.QEncoding.Encode("UTF-8", d.Subject), body)
	if err != nil {
		return fmt.Errorf("SMTP write failed")
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("SMTP delivery failed")
	}
	if err = c.Quit(); err != nil {
		return fmt.Errorf("SMTP accepted the message but closing the connection failed; do not resend before checking delivery")
	}
	return nil
}

type App struct {
	Backend Backend
	Account string
	mu      sync.Mutex
	drafts  map[string]Draft
}

func NewApp(b Backend, account string) *App {
	return &App{Backend: b, Account: account, drafts: map[string]Draft{}}
}
func (a *App) Target(box string, valid, uid uint32) string {
	return a.Account + "/" + box + "/" + strconv.FormatUint(uint64(valid), 10) + "/" + strconv.FormatUint(uint64(uid), 10)
}
func (a *App) Call(ctx context.Context, target, name string, args json.RawMessage) (any, error) {
	switch name {
	case "mail_draft":
		var d Draft
		if err := json.Unmarshal(args, &d); err != nil {
			return nil, fmt.Errorf("invalid draft")
		}
		if err := validateDraft(d); err != nil {
			return nil, err
		}
		a.mu.Lock()
		a.drafts[target] = d
		a.mu.Unlock()
		return map[string]any{"saved": true, "sent": false}, nil
	case "mail_read":
		var p struct {
			Mailbox     string `json:"mailbox"`
			UIDValidity uint32 `json:"uidValidity"`
			UID         uint32 `json:"uid"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, err
		}
		if p.Mailbox == "" && p.UIDValidity == 0 && p.UID == 0 {
			if !strings.HasPrefix(target, a.Account+"/") {
				return nil, fmt.Errorf("message does not belong to this account")
			}
			parts := strings.Split(strings.TrimPrefix(target, a.Account+"/"), "/")
			if len(parts) < 3 {
				return nil, fmt.Errorf("invalid message target")
			}
			validity, err := strconv.ParseUint(parts[len(parts)-2], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid mailbox identity")
			}
			uid, err := strconv.ParseUint(parts[len(parts)-1], 10, 32)
			if err != nil || validity == 0 || uid == 0 {
				return nil, fmt.Errorf("invalid message identity")
			}
			p.Mailbox = strings.Join(parts[:len(parts)-2], "/")
			p.UIDValidity = uint32(validity)
			p.UID = uint32(uid)
		}
		if target != a.Target(p.Mailbox, p.UIDValidity, p.UID) {
			return nil, fmt.Errorf("message does not belong to this session")
		}
		return a.Backend.Read(ctx, p.Mailbox, p.UIDValidity, p.UID)
	}
	return nil, fmt.Errorf("unknown mail tool")
}
func (a *App) HTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var out any
	var err error
	switch r.URL.Path {
	case "/mail/list":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		box := r.URL.Query().Get("mailbox")
		if box == "" {
			box = "INBOX"
		}
		var valid uint32
		var items []Item
		valid, items, err = a.Backend.List(r.Context(), box)
		out = map[string]any{"uidValidity": valid, "items": items, "account": a.Account}
	case "/mail/read":
		var p struct {
			Mailbox     string `json:"mailbox"`
			UIDValidity uint32 `json:"uidValidity"`
			UID         uint32 `json:"uid"`
		}
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p)
		if err == nil {
			out, err = a.Backend.Read(r.Context(), p.Mailbox, p.UIDValidity, p.UID)
		}
	case "/mail/draft":
		target := r.URL.Query().Get("target")
		a.mu.Lock()
		out = a.drafts[target]
		a.mu.Unlock()
	case "/mail/send":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var p struct {
			Draft   Draft `json:"draft"`
			Confirm bool  `json:"confirm"`
		}
		err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p)
		if err == nil && !p.Confirm {
			err = fmt.Errorf("explicit send confirmation required")
		}
		if err == nil {
			err = a.Backend.Send(r.Context(), p.Draft)
		}
		out = map[string]bool{"sent": err == nil}
	default:
		w.WriteHeader(404)
		return
	}
	if err != nil {
		w.WriteHeader(400)
		out = map[string]string{"error": err.Error()}
	}
	json.NewEncoder(w).Encode(out)
}

func decodeTransfer(encoding string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	}
	return r
}

func (n Network) tlsFor(host string) *tls.Config {
	if n.tlsConfig != nil {
		c := n.tlsConfig.Clone()
		c.ServerName = host
		return c
	}
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}
