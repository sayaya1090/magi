package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/core/companion"
)

// Self-description fills missing metadata only. Access, operations and user instructions are
// preserved from the validated policy; unknown JSON fields cannot become authority.
func companionProfileCommand(configDir, workdir string, in io.Reader, out, errOut io.Writer) int {
	var profile struct {
		Purpose          string           `json:"purpose"`
		Responsibilities []string         `json:"responsibilities"`
		Tasks            []companion.Task `json:"tasks"`
	}
	data, err := io.ReadAll(io.LimitReader(in, companionpolicy.MaxFileBytes+1))
	if err != nil || len(data) > companionpolicy.MaxFileBytes {
		fmt.Fprintln(errOut, "profile exceeds limit or cannot be read")
		return 1
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&profile); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		fmt.Fprintln(errOut, "expected one JSON profile")
		return 1
	}
	if strings.TrimSpace(profile.Purpose) == "" || len(profile.Tasks) == 0 {
		fmt.Fprintln(errOut, "purpose and at least one concrete task are required")
		return 1
	}
	// Validate proposed metadata even when populated sections would later be preserved.
	var proposed bytes.Buffer
	if err = toml.NewEncoder(&proposed).Encode(map[string]any{"version": int64(1), "purpose": profile.Purpose, "responsibilities": profile.Responsibilities, "tasks": profile.Tasks}); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	stage, err := os.MkdirTemp("", "magi-profile-check-")
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer os.RemoveAll(stage)
	if companionPolicyCommand(stage, workdir, true, &proposed, io.Discard, errOut) != 0 {
		return 1
	}
	unlock, err := lockCompanionPolicy(configDir, workdir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer unlock()
	current, err := companionpolicy.Load(configDir, workdir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	path, err := companionpolicy.Path(configDir, workdir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if current.Purpose != "" && len(current.Tasks) > 0 && (len(current.Responsibilities) > 0 || len(profile.Responsibilities) == 0) {
		fmt.Fprintln(out, "Existing companion description preserved; no empty sections to fill")
		return 0
	}
	raw := map[string]any{"version": int64(1)}
	if _, err = os.Stat(path); err == nil {
		if _, err = toml.DecodeFile(path, &raw); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	} else if !os.IsNotExist(err) {
		fmt.Fprintln(errOut, err)
		return 1
	}
	// Never replace a populated section, including metadata a user subsequently edited.
	if current.Purpose == "" {
		raw["purpose"] = profile.Purpose
	}
	if len(current.Responsibilities) == 0 {
		raw["responsibilities"] = profile.Responsibilities
	}
	if len(current.Tasks) == 0 {
		raw["tasks"] = profile.Tasks
	}
	var encoded bytes.Buffer
	if err = toml.NewEncoder(&encoded).Encode(raw); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return companionPolicyCommandUnlocked(configDir, workdir, true, &encoded, out, errOut)
}
