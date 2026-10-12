package app

import (
	"fmt"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// SetCouncil queues a session-specific choice; a running turn keeps its snapshot.
func (a *App) SetCouncil(sid session.SessionID, enabled bool) error {
	if enabled && a.cfg.Council == nil {
		return fmt.Errorf("카운슬 검토기가 구성되지 않았습니다.")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stateLocked(sid).councilWanted = &enabled
	return nil
}

func (a *App) CouncilSetting(sid session.SessionID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	enabled := a.cfg.Council != nil
	if a.cfg.CouncilInitiallyEnabled != nil {
		enabled = enabled && *a.cfg.CouncilInitiallyEnabled
	}
	if st, ok := a.stateIf(sid); ok && st.councilWanted != nil {
		enabled = *st.councilWanted
	}
	return enabled
}
func (a *App) snapshotCouncil(sid session.SessionID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	enabled := a.cfg.Council != nil
	if a.cfg.CouncilInitiallyEnabled != nil {
		enabled = enabled && *a.cfg.CouncilInitiallyEnabled
	}
	if st.councilWanted != nil {
		enabled = *st.councilWanted
	}
	st.councilTurn = &enabled
}
func (a *App) councilFor(sid session.SessionID) port.Council {
	a.mu.Lock()
	defer a.mu.Unlock()
	enabled := a.cfg.Council != nil
	if a.cfg.CouncilInitiallyEnabled != nil {
		enabled = enabled && *a.cfg.CouncilInitiallyEnabled
	}
	if st, ok := a.stateIf(sid); ok {
		if st.councilTurn != nil {
			enabled = *st.councilTurn
		} else if st.councilWanted != nil {
			enabled = *st.councilWanted
		}
	}
	if !enabled {
		return nil
	}
	return a.cfg.Council
}
