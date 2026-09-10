package main

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// A seat is found by which SOCKET it is, not by how the page happened to spell it.
//
// `companionSeat` compared the form field against the published set with `!=`. Those are spelled by
// different hands: the field is whatever the page sent, and the published spelling comes back from
// the glob in the OS's own separator. One `/` where Windows writes `\` and the console answers
// "this console has no companion you may convene at …" about a companion it is listing on the same
// screen. Measured 2026-09-10 — twelve meeting tests, all of them that convene anything.
//
// `daemon.Find`, in this same file, already went through the normalising door. This was the copy
// that did not.
//
// ⚠ And the seat carries the PUBLISHED path onward, not the caller's. A seat is dialled; matching
// loosely is safe only because what gets opened is still the path this console published, never the
// form field.
func TestASeatIsFoundHoweverThePageSpeltItsSocket(t *testing.T) {
	f, _, _, who := room(t)
	published := who["who"][0]

	carried := map[string]string{}
	for name, spelling := range map[string]string{
		"as the fixture spelt it": published,
		"with a slash":            filepath.ToSlash(published),
		"with a doubled sep":      filepath.Dir(published) + string(filepath.Separator) + string(filepath.Separator) + filepath.Base(published),
	} {
		seat, ok := f.srv.companionSeat(httptest.NewRequest("GET", "/meet", nil), spelling)
		if !ok {
			t.Errorf("%s (%s): 이 콘솔이 목록에 띄우고 있는 컴패니언을 못 찾았다", name, spelling)
			continue
		}
		if seat.Name != "design" {
			t.Errorf("%s: 다른 자리를 찾았다: %+v", name, seat)
		}
		if !daemon.SamePath(seat.Socket, published) {
			t.Errorf("%s: 자리가 다른 파일을 가리킨다: %q", name, seat.Socket)
		}
		carried[name] = seat.Socket
	}
	// ONE value, whatever it was asked with — the console's own, not the caller's. A seat gets
	// dialled, so three spellings in must not become three different things opened.
	var first, firstName string
	for name, got := range carried {
		if first == "" {
			first, firstName = got, name
			continue
		}
		if got != first {
			t.Errorf("부른 철자마다 다른 것을 들고 간다: %s→%q vs %s→%q", firstName, first, name, got)
		}
	}

	// Loosening the spelling must not loosen the gate: a socket nobody published is still nobody.
	for name, bad := range map[string]string{
		"never published": filepath.Join(filepath.Dir(published), "daemon-nope.sock"),
		"a directory up":  filepath.Dir(published),
		"empty":           "",
	} {
		if seat, ok := f.srv.companionSeat(httptest.NewRequest("GET", "/meet", nil), bad); ok {
			t.Errorf("%s (%s) 를 자리로 받아 줬다: %+v", name, bad, seat)
		}
	}

	// And the door above it says so in words rather than resolving something else.
	form := url.Values{"who": {filepath.Join(filepath.Dir(published), "daemon-nope.sock")},
		"topic": {"anything"}, "rounds": {"1"}}
	w := post(t, f.srv, f.srv.meet, "/meet", form)
	if w.Code == 200 {
		t.Error("발행되지 않은 소켓으로 회의가 열렸다")
	}
	if !strings.Contains(w.Body.String(), "no companion you may convene") {
		t.Errorf("거절이 제 이름을 안 댄다: %s", w.Body.String())
	}
}
