package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubController struct {
	triggerSyncErr error
	snoozeErr      error
	snoozeExpires  time.Time
	resumeErr      error
}

func (s stubController) RepoStatus(string) ([]RepoInfo, error) {
	return nil, nil
}

func (s stubController) TriggerSync(string) error {
	return s.triggerSyncErr
}

func (s stubController) SnoozeRepo(string, time.Duration) (time.Time, error) {
	return s.snoozeExpires, s.snoozeErr
}

func (s stubController) ResumeRepo(string) error {
	return s.resumeErr
}

func TestHandlerSyncSuccess(t *testing.T) {
	h := &handler{ctrl: stubController{}}

	resp, err := h.Sync(context.Background(), &sextonv1.SyncRequest{Repo: "repo"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if got := resp.GetMessage(); got != "sync triggered" {
		t.Fatalf("Sync() message = %q, want %q", got, "sync triggered")
	}
}

func TestHandlerSyncFailureUsesRPCError(t *testing.T) {
	h := &handler{ctrl: stubController{triggerSyncErr: errors.New("agent is snoozed")}}

	resp, err := h.Sync(context.Background(), &sextonv1.SyncRequest{Repo: "repo"})
	if resp != nil {
		t.Fatalf("Sync() resp = %#v, want nil on error", resp)
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Sync() code = %v, want %v", status.Code(err), codes.FailedPrecondition)
	}
}

func TestHandlerSnoozeSuccess(t *testing.T) {
	expires := time.Date(2026, time.March, 18, 18, 0, 0, 0, time.UTC)
	h := &handler{ctrl: stubController{snoozeExpires: expires}}

	resp, err := h.Snooze(context.Background(), &sextonv1.SnoozeRequest{
		Repo:     "repo",
		Duration: "1h",
	})
	if err != nil {
		t.Fatalf("Snooze() error = %v", err)
	}
	if got := resp.GetExpires(); got != expires.Format(time.RFC3339) {
		t.Fatalf("Snooze() expires = %q, want %q", got, expires.Format(time.RFC3339))
	}
}

func TestHandlerSnoozeInvalidDurationUsesRPCError(t *testing.T) {
	h := &handler{ctrl: stubController{}}

	resp, err := h.Snooze(context.Background(), &sextonv1.SnoozeRequest{
		Repo:     "repo",
		Duration: "later",
	})
	if resp != nil {
		t.Fatalf("Snooze() resp = %#v, want nil on error", resp)
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Snooze() code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
}

func TestHandlerSnoozeFailureUsesRPCError(t *testing.T) {
	h := &handler{ctrl: stubController{snoozeErr: errors.New("agent is snoozed")}}

	resp, err := h.Snooze(context.Background(), &sextonv1.SnoozeRequest{
		Repo:     "repo",
		Duration: "1h",
	})
	if resp != nil {
		t.Fatalf("Snooze() resp = %#v, want nil on error", resp)
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Snooze() code = %v, want %v", status.Code(err), codes.FailedPrecondition)
	}
}

func TestHandlerResumeSuccess(t *testing.T) {
	h := &handler{ctrl: stubController{}}

	resp, err := h.Resume(context.Background(), &sextonv1.ResumeRequest{Repo: "repo"})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if got := resp.GetMessage(); got != "resumed" {
		t.Fatalf("Resume() message = %q, want %q", got, "resumed")
	}
}

func TestHandlerResumeFailureUsesRPCError(t *testing.T) {
	h := &handler{ctrl: stubController{resumeErr: errors.New("agent is not errored or snoozed")}}

	resp, err := h.Resume(context.Background(), &sextonv1.ResumeRequest{Repo: "repo"})
	if resp != nil {
		t.Fatalf("Resume() resp = %#v, want nil on error", resp)
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Resume() code = %v, want %v", status.Code(err), codes.FailedPrecondition)
	}
}

func TestOperationErrorMapsRepoNotFound(t *testing.T) {
	err := operationError(errors.Join(ErrRepoNotFound, errors.New(`repo "missing"`)))
	if status.Code(err) != codes.NotFound {
		t.Fatalf("operationError() code = %v, want %v", status.Code(err), codes.NotFound)
	}
}
