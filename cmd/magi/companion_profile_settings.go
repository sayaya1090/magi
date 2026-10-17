package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/companion"
)

type editableCompanionProfile struct {
	Purpose          string           `json:"purpose"`
	Responsibilities []string         `json:"responsibilities"`
	Tasks            []companion.Task `json:"tasks"`
}

func companionProfileNeeded(configDir, wd string) bool {
	d := daemonEngine{configDir: configDir, workdir: wd}
	key, _ := settingKeyOf("companion_auto_describe")
	mode := d.resolve(key)
	// Malformed settings must not silently enable a paid background request.
	if mode.Unreadable != "" || mode.Value != "on" {
		return false
	}
	c, err := companionpolicy.Load(configDir, wd)
	return err == nil && (c.Purpose == "" || len(c.Tasks) == 0)
}

func (d daemonEngine) profileSetting() (daemon.ConfigItem, error) {
	c, err := companionpolicy.Load(d.configDir, d.workdir)
	if err != nil {
		return daemon.ConfigItem{}, err
	}
	b, err := json.Marshal(editableCompanionProfile{c.Purpose, c.Responsibilities, c.Tasks})
	if err != nil {
		return daemon.ConfigItem{}, err
	}
	path, err := companionpolicy.Path(d.configDir, d.workdir)
	if err != nil {
		return daemon.ConfigItem{}, err
	}
	return daemon.ConfigItem{Key: "companion.profile", Value: string(b), Source: "companion", Tier: "companion", File: path, Applies: "now", Doc: "Workspace purpose, responsibilities and offered tasks. Editable JSON; does not grant access or publish callable operations."}, err
}

func (d daemonEngine) setProfileSetting(value string) (daemon.ConfigItem, error) {
	if len(value) > companionpolicy.MaxFileBytes {
		return daemon.ConfigItem{}, fmt.Errorf("profile exceeds limit")
	}
	if !strings.HasPrefix(strings.TrimSpace(value), "{") {
		return daemon.ConfigItem{}, fmt.Errorf("expected a profile object")
	}
	var p editableCompanionProfile
	dec := json.NewDecoder(strings.NewReader(value))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return daemon.ConfigItem{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return daemon.ConfigItem{}, fmt.Errorf("expected one profile object")
	}
	unlock, err := lockCompanionPolicy(d.configDir, d.workdir)
	if err != nil {
		return daemon.ConfigItem{}, err
	}
	defer unlock()
	path, err := companionpolicy.Path(d.configDir, d.workdir)
	if err != nil {
		return daemon.ConfigItem{}, err
	}
	raw := map[string]any{"version": int64(1)}
	if _, err = os.Stat(path); err == nil {
		if _, err = toml.DecodeFile(path, &raw); err != nil {
			return daemon.ConfigItem{}, err
		}
	} else if !os.IsNotExist(err) {
		return daemon.ConfigItem{}, err
	}
	raw["purpose"], raw["responsibilities"], raw["tasks"] = p.Purpose, p.Responsibilities, p.Tasks
	var b, why bytes.Buffer
	if err = toml.NewEncoder(&b).Encode(raw); err != nil {
		return daemon.ConfigItem{}, err
	}
	if companionPolicyCommandUnlocked(d.configDir, d.workdir, true, &b, io.Discard, &why) != 0 {
		return daemon.ConfigItem{}, fmt.Errorf("%s", why.String())
	}
	// The periodic publisher also repairs records when a daemon is not yet published.
	if err := refreshCompanionAdvertisement(d.configDir, d.workdir, daemon.SocketPath(d.configDir, d.workdir), ""); err != nil {
		log.Printf("companion profile: saved; advertisement refresh pending: %v", err)
	}
	return d.profileSetting()
}

func companionProfileUnpublished(configDir, wd string) bool {
	if !companionProfileNeeded(configDir, wd) {
		return false
	}
	c, err := companionpolicy.Load(configDir, wd)
	return err == nil && len(c.Tasks) == 0 && len(c.Operations) == 0
}
