package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// The page paints diagrams with the palette the aimed daemon resolved from the person's [theme].
func TestTheConsoleReadsTheDaemonsTheme(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.liveDaemon(t, wd, "th", &themedEngine{})
	f.session("th", wd, "draw it", 1, false)
	w := get(t, f.srv.theme, "/theme?d="+url.QueryEscape(sock))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /theme replied %d: %s", w.Code, w.Body.String())
	}
	var got struct{ Dark, Light map[string]string }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	if got.Dark["primary"] != "#123456" || got.Light["primary"] != "#ABCDEF" {
		t.Fatalf("the page was told %s", w.Body.String())
	}
	if !openToRead["/theme"] {
		t.Error("/theme only reads and must be listed as such")
	}
}

type themedEngine struct{ recordingEngine }

func (*themedEngine) ThemePalettes() (dark, light map[string]string) {
	return map[string]string{"primary": "#123456"}, map[string]string{"primary": "#ABCDEF"}
}
