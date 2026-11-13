package app

import "errors"

// Admission covers persistence as well as run creation. Close must not report
// drained while an accepted prompt is still writing to the store.
var ErrShuttingDown = errors.New("magi is shutting down; prompt was not accepted")
var ErrUpdateHeld = errors.New("magi is restarting for an update; retry the prompt after reconnecting")

func (a *App) beginPromptAdmission() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrShuttingDown
	}
	if a.holding {
		return ErrUpdateHeld
	}
	a.promptAdmissions++
	a.wg.Add(1)
	return nil
}

func (a *App) endPromptAdmission() {
	a.mu.Lock()
	a.promptAdmissions--
	a.mu.Unlock()
	a.wg.Done()
}
