package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/core/session"
)

func (d daemonEngine) ProfileMaintenanceStatus(ctx context.Context, sid string, refresh bool) (string, error) {
	if refresh {
		if !companionMaintenanceEnabled(d.configDir, d.workdir) {
			return "", fmt.Errorf("automatic description maintenance is disabled")
		}
		if sid == "" {
			return "", fmt.Errorf("select a source chat before requesting maintenance")
		}
		if d.App == nil {
			return "", fmt.Errorf("companion engine unavailable")
		}
		if err := d.App.RequestProfileMaintenance(ctx, session.SessionID(sid), d.workdir); err != nil {
			return "", err
		}
	}
	state, err := (&companionpolicy.Maintainer{ConfigDir: d.configDir}).ProfileMaintenanceState(ctx, d.workdir)
	if err != nil {
		return "", err
	}
	state.Enabled = companionMaintenanceEnabled(d.configDir, d.workdir)
	if d.App != nil {
		state.Pending = d.App.ProfileMaintenancePending() && !state.Running
	}
	b, err := json.Marshal(state)
	return string(b), err
}
