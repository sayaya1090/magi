package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
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

func companionSelfDescriptionGuide(workdir, executable string) string {
	if workdir == "" || executable == "" {
		return ""
	}
	quote := func(s string) string {
		if runtime.GOOS == "windows" {
			return "'" + strings.ReplaceAll(s, "'", "''") + "'"
		}
		return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
	}
	command := quote(executable)
	if runtime.GOOS == "windows" {
		command = "& " + quote(executable)
	}
	return `

# Describe your workspace to other companions
After understanding the workspace from actual project files, documentation and tools, fill missing companion metadata with a concrete purpose, responsibilities and offered tasks. Do this when enough is known, not on every turn and not merely because a learned skill exists. Do not interrupt an urgent user request or invent capabilities, successful verification, access grants or directly callable operations.
Read the existing declaration first with ` + command + ` --companion-policy-show, using ` + quote(workdir) + ` as the working directory. Keep existing user descriptions and constraints. If purpose and tasks are already populated, do not rewrite them. A prepared coding-task runner must not publish itself as a new companion.
For missing sections, send one JSON object on stdin to ` + command + ` --companion-profile-write in the same directory. Schema: {"purpose":"what this workspace is for","responsibilities":["what you maintain"],"tasks":[{"id":"short-stable-id","description":"specific work peers can request, with inputs and expected result","constraints":["scope or verification limits"]}]}. Use the user's language. This writer fills empty sections only and preserves access, operations and instructions. Never use --companion-policy-write to grant yourself access or publish operations.
Re-read --companion-policy-show to verify what was saved. Report the new offered tasks briefly when relevant. A failed save does not mean publication succeeded; do not retry unchanged errors in a loop. No model or network request is needed just to store the metadata.
`
}
