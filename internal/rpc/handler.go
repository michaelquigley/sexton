package rpc

import (
	"context"
	"errors"
	"time"

	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type handler struct {
	sextonv1.UnimplementedSextonServer
	ctrl AgentController
}

func (h *handler) Status(_ context.Context, req *sextonv1.StatusRequest) (*sextonv1.StatusResponse, error) {
	infos, err := h.ctrl.RepoStatus(req.GetRepo())
	if err != nil {
		return nil, operationError(err)
	}

	resp := &sextonv1.StatusResponse{}
	for _, info := range infos {
		rs := &sextonv1.RepoStatus{
			Path:       info.Path,
			Name:       info.Name,
			State:      info.State,
			Branch:     info.Branch,
			LastCommit: info.LastCommit,
		}
		if !info.LastSync.IsZero() {
			rs.LastSync = info.LastSync.Format(time.RFC3339)
		}
		if !info.LastChange.IsZero() {
			rs.LastChange = info.LastChange.Format(time.RFC3339)
		}
		if info.Error != "" {
			rs.Error = info.Error
		}
		if info.SnoozeRemaining > 0 {
			rs.SnoozeRemaining = info.SnoozeRemaining.Round(time.Second).String()
		}
		resp.Repos = append(resp.Repos, rs)
	}
	return resp, nil
}

func (h *handler) Sync(_ context.Context, req *sextonv1.SyncRequest) (*sextonv1.SyncResponse, error) {
	if err := h.ctrl.TriggerSync(req.GetRepo()); err != nil {
		return nil, operationError(err)
	}
	return &sextonv1.SyncResponse{Message: "sync triggered"}, nil
}

func (h *handler) Snooze(_ context.Context, req *sextonv1.SnoozeRequest) (*sextonv1.SnoozeResponse, error) {
	d, err := time.ParseDuration(req.GetDuration())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid snooze duration %q: %v", req.GetDuration(), err)
	}

	expires, err := h.ctrl.SnoozeRepo(req.GetRepo(), d)
	if err != nil {
		return nil, operationError(err)
	}

	return &sextonv1.SnoozeResponse{Expires: expires.Format(time.RFC3339)}, nil
}

func (h *handler) Resume(_ context.Context, req *sextonv1.ResumeRequest) (*sextonv1.ResumeResponse, error) {
	if err := h.ctrl.ResumeRepo(req.GetRepo()); err != nil {
		return nil, operationError(err)
	}
	return &sextonv1.ResumeResponse{Message: "resumed"}, nil
}

func operationError(err error) error {
	if errors.Is(err, ErrRepoNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, ErrAmbiguousRepo) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return status.Error(codes.FailedPrecondition, err.Error())
}
