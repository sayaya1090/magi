package port

import (
	"context"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
)

// CodingTasks always receives the trusted caller workspace, not a model path.
type CodingTasks interface {
	Prepare(context.Context, string, task.Request) (task.Record, error)
	List(context.Context, string) ([]task.Record, error)
	Status(context.Context, string, string) (task.Record, error)
	Review(context.Context, string, string, string) (task.Review, error)
	Diff(context.Context, string, string) ([]task.Change, error)
	Apply(context.Context, string, string, []string) (task.Journal, error)
	Restore(context.Context, string, string) (task.Journal, error)
}
