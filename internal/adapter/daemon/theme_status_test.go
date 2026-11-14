package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The status answer carries the colour roles this daemon's [theme] resolves to, so a viewer drawing
// a diagram paints it with the person's palette rather than a copy compiled into the client.
func TestStatusCarriesTheResolvedTheme(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	for _, tc := range []struct {
		name string
		eng  Engine
		want bool
	}{{"teller", &themeSayingEngine{}, true}, {"plain", &omniEngine{}, false}} {
		sock := filepath.Join(home, "daemon-"+tc.name+".sock")
		d, err := Listen(sock)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(d.Stop)
		eng := tc.eng
		go func() { _ = d.Serve(context.Background(), eng) }()
		c, err := Dial(sock)
		if err != nil {
			t.Fatal(err)
		}
		st, err := c.Status("s_1")
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !tc.want {
			if st.Theme != nil {
				t.Errorf("%s: an engine that cannot say invented a theme: %+v", tc.name, st.Theme)
			}
			continue
		}
		if st.Theme == nil || st.Theme.Dark["primary"] != "#123456" || st.Theme.Light["primary"] != "#ABCDEF" {
			t.Errorf("%s: status theme = %+v", tc.name, st.Theme)
		}
	}
}

type themeSayingEngine struct{ omniEngine }

func (*themeSayingEngine) ThemePalettes() (dark, light map[string]string) {
	return map[string]string{"primary": "#123456"}, map[string]string{"primary": "#ABCDEF"}
}
