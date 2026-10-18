package daemon

import "context"

type ProfileMaintenanceHost interface {
	ProfileMaintenanceStatus(context.Context, string, bool) (string, error)
}

func answerProfileMaintenance(ctx context.Context, eng Engine, req Request) Response {
	host, ok := eng.(ProfileMaintenanceHost)
	if !ok {
		return Response{Err: "this companion does not offer internal description maintenance"}
	}
	if req.Name != "" && req.Name != "refresh" {
		return Response{Err: "unknown maintenance action"}
	}
	out, err := host.ProfileMaintenanceStatus(ctx, req.Session, req.Name == "refresh")
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Out: out}
}
