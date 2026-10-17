package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/atomicfile"
)

// Only an explicit local CLI action writes authority. No model tool or fleet method exposes this.
func companionPolicyCommand(configDir, workdir string, write bool, in io.Reader, out, errOut io.Writer) int {
	if !write {
		c, err := companionpolicy.Load(configDir, workdir)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		e := json.NewEncoder(out)
		e.SetIndent("", "  ")
		if err = e.Encode(c); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}
	data, err := io.ReadAll(io.LimitReader(in, companionpolicy.MaxFileBytes+1))
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if len(data) > companionpolicy.MaxFileBytes {
		fmt.Fprintln(errOut, "policy exceeds 1 MiB")
		return 1
	}
	stage, err := os.MkdirTemp("", "magi-policy-check-")
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer os.RemoveAll(stage)
	staged, err := companionpolicy.Path(stage, workdir)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(staged), 0700)
	}
	if err == nil {
		err = os.WriteFile(staged, data, 0600)
	}
	if err == nil {
		_, err = companionpolicy.Load(stage, workdir)
	}
	if err != nil {
		fmt.Fprintln(errOut, "declaration not saved:", err)
		return 1
	}
	target, err := companionpolicy.Path(configDir, workdir)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(target), 0700)
	}
	if err == nil {
		if info, e := os.Lstat(target); e == nil && !info.Mode().IsRegular() {
			err = fmt.Errorf("policy must be a regular file")
		} else if e != nil && !os.IsNotExist(e) {
			err = e
		}
	}
	if err == nil {
		err = atomicfile.Write(target, data, 0600)
	}
	if err != nil {
		fmt.Fprintln(errOut, "declaration not saved:", err)
		return 1
	}
	fmt.Fprintln(out, "Saved companion declaration:", target)
	return 0
}
