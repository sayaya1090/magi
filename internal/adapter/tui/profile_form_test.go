package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The profile sub-editor sat at 0% — a whole full-screen view that had never been rendered in a
// test. It is reached by ordinary keys (/route → the "add profile" row → enter), it takes typed
// input, and one of its fields is an API key.

// openProfileForm walks the real keys to the form: /route, down to the add-profile row, enter.
func openProfileForm(t *testing.T, s *script) {
	t.Helper()
	s.typeText("/route").enter()
	if !s.m.routing {
		t.Fatal("/route did not open the editor")
	}
	for i := 0; i < len(s.m.routeList); i++ {
		if s.m.routeList[s.m.routeSel].kind == rowAddProfile {
			break
		}
		s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if s.m.routeList[s.m.routeSel].kind != rowAddProfile {
		t.Fatalf("never reached the add-profile row; rows: %d", len(s.m.routeList))
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.m.profileForm == nil {
		t.Fatal("enter on the add-profile row opened no form")
	}
}

// A base_url or a model name is routinely longer than a narrow terminal, and the form lays its
// rows out with a fixed-width label column and no clip. One over-wide row in a vertically joined
// frame pads every other row to match, so the whole screen goes wider than the terminal.
func TestTheProfileFormFitsItsTerminal(t *testing.T) {
	for _, w := range []int{34, 48, 72, 110} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: w, Height: 30})
		openProfileForm(t, s)

		// Type a realistic value into the first field.
		s.send(tea.KeyPressMsg{Code: tea.KeyEnter}) // start editing the selected field
		for _, r := range "https://gateway.internal.example.com:8443/v1/openai-compatible" {
			s.send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		s.send(tea.KeyPressMsg{Code: tea.KeyEnter}) // commit

		raw := s.rawView()
		for i, line := range strings.Split(raw, "\n") {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(line, ""), " ")
			if got := lipgloss.Width(trimmed); got > w {
				t.Errorf("width %d: row %d draws %d cells: %q", w, i, got, trimmed)
			}
		}
		if rows := len(strings.Split(raw, "\n")); rows > s.m.height {
			t.Errorf("width %d: the frame is %d rows in a %d-row terminal (chrome=%d vp=%d)",
				w, rows, s.m.height, s.m.chromeHeight(), s.m.vp.Height())
		}
	}
}

// The api_key field is masked unless it is the one being edited. A key echoed into a transcript
// that gets pasted into a bug report is the kind of leak nothing else in the UI can undo.
func TestTheApiKeyIsMaskedUnlessItIsBeingEdited(t *testing.T) {
	const secret = "sk-do-not-render-this-anywhere"
	s := newScript(t)
	openProfileForm(t, s)

	// Walk to api_key and type it in.
	at := -1
	for i, f := range s.m.profileForm.fields {
		if f.label == "api_key" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the form has no api_key field")
	}
	for s.m.profileForm.sel < at {
		s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, r := range secret {
		s.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	// While editing, the field shows what is being typed — masking your own input is unusable.
	if !strings.Contains(s.view(), secret) {
		t.Error("the field being edited does not echo what is typed")
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyEnter}) // commit and stop editing

	if got := s.view(); strings.Contains(got, secret) {
		t.Errorf("the api key is on screen once it is no longer the field being edited:\n%s", got)
	}
	if s.m.profileForm.fields[at].value != secret {
		t.Errorf("masking must be a display choice, not a lost value: %q", s.m.profileForm.fields[at].value)
	}
}

// esc backs out one level at a time — form → list → transcript. A view that swallows esc, or one
// that drops straight to the transcript, are both ways to lose typed work with no way back.
func TestEscLeavesTheFormBeforeTheEditor(t *testing.T) {
	s := newScript(t)
	openProfileForm(t, s)

	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.m.profileForm != nil {
		t.Error("esc did not leave the form")
	}
	if !s.m.routing {
		t.Error("esc from the form left the route editor too — one press, two levels")
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.m.routing {
		t.Error("a second esc did not leave the route editor")
	}
}

// The form reserves its own rows in the chrome, the same way the modals do. It is drawn where the
// palette would be, and every surface there that reserves nothing pushes the frame off the bottom.
func TestTheProfileFormReservesTheRowsItDraws(t *testing.T) {
	for _, h := range []int{14, 22, 40} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 90, Height: h})
		openProfileForm(t, s)
		drawn := lipgloss.Height(s.m.profileFormView())
		reserved := len(s.m.profileForm.fields) + 3
		if reserved < drawn {
			t.Errorf("height %d: the form draws %d rows and reserves %d", h, drawn, reserved)
		}
		if rows := len(strings.Split(s.rawView(), "\n")); rows > h {
			t.Errorf("height %d: the frame is %d rows", h, rows)
		}
	}
}
