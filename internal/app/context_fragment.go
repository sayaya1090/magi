package app

// Context metadata is application-owned. Neither source nor lane grants authority.
type contextLane uint8

const (
	turnSystem contextLane = iota
	volatileStable
	volatileRun
	volatileClock
)

type contextFragment struct {
	id       string
	source   string
	text     string
	lane     contextLane
	requires []string
}

type contextDecision struct {
	ID     string
	Lane   contextLane
	Bytes  int
	State  string
	Reason string
}
