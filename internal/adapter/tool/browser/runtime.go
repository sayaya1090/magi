package browser

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
)

// Runtime is a local capability record, never model-provided credentials.
type Runtime struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func runtimePath(dir, mode string) (string, error) {
	if mode != "headless" && mode != "extension" {
		return "", errors.New("browser mode must be headless or extension")
	}
	return filepath.Join(dir, "runtime", "browser-"+mode+".json"), nil
}

func (r Runtime) validate() error {
	u, err := url.Parse(r.URL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("invalid local browser endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" || len(r.Token) < 16 {
		return errors.New("browser endpoint must be authenticated loopback")
	}
	return nil
}

// Publish stores credentials owner-only and removes only this instance on cleanup.
func Publish(dir, mode string, record Runtime) (func(), error) {
	path, err := runtimePath(dir, mode)
	if err != nil {
		return nil, err
	}
	if err = record.validate(); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".browser-")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	data, _ := json.Marshal(record)
	if _, err = f.Write(data); err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		return nil, err
	}
	if err = os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return func() {
		current, e := readRuntime(dir, mode)
		if e == nil && current == record {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("browser runtime cleanup: %v", err)
			}
		}
	}, nil
}

func readRuntime(dir, mode string) (Runtime, error) {
	var record Runtime
	path, err := runtimePath(dir, mode)
	if err != nil {
		return record, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return record, errors.New("browser helper unavailable; start magi-browser in the requested mode first")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return record, errors.New("browser runtime credentials must be a small owner-only regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return record, errors.New("cannot read browser runtime registration")
	}
	if json.Unmarshal(data, &record) != nil {
		return record, errors.New("invalid browser runtime registration")
	}
	return record, record.validate()
}
