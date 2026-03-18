package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeAgentConn struct{}

func (fakeAgentConn) Close() error { return nil }

type fakeSextonClient struct {
	statusResp *sextonv1.StatusResponse
	statusErr  error
	syncResp   *sextonv1.SyncResponse
	syncErr    error
	snoozeResp *sextonv1.SnoozeResponse
	snoozeErr  error
	resumeResp *sextonv1.ResumeResponse
	resumeErr  error
}

func (f fakeSextonClient) Status(context.Context, *sextonv1.StatusRequest, ...grpc.CallOption) (*sextonv1.StatusResponse, error) {
	if f.statusResp == nil && f.statusErr == nil {
		return nil, errors.New("unexpected Status call")
	}
	return f.statusResp, f.statusErr
}

func (f fakeSextonClient) Sync(context.Context, *sextonv1.SyncRequest, ...grpc.CallOption) (*sextonv1.SyncResponse, error) {
	return f.syncResp, f.syncErr
}

func (f fakeSextonClient) Snooze(context.Context, *sextonv1.SnoozeRequest, ...grpc.CallOption) (*sextonv1.SnoozeResponse, error) {
	return f.snoozeResp, f.snoozeErr
}

func (f fakeSextonClient) Resume(context.Context, *sextonv1.ResumeRequest, ...grpc.CallOption) (*sextonv1.ResumeResponse, error) {
	return f.resumeResp, f.resumeErr
}

func TestRunSyncSuccess(t *testing.T) {
	restore := stubDialAgent(t, fakeSextonClient{
		syncResp: &sextonv1.SyncResponse{Message: "sync triggered"},
	}, nil)
	defer restore()

	output := captureStdout(t, func() {
		if err := runSync(nil, []string{"repo"}); err != nil {
			t.Fatalf("runSync() error = %v", err)
		}
	})

	if output != "sync triggered\n" {
		t.Fatalf("runSync() output = %q, want %q", output, "sync triggered\n")
	}
}

func TestRunStatusAmbiguousRepoReturnsError(t *testing.T) {
	restore := stubDialAgent(t, fakeSextonClient{
		statusErr: status.Error(codes.InvalidArgument, `ambiguous repo "grimoire"`),
	}, nil)
	defer restore()

	output := captureStdout(t, func() {
		err := runStatus(nil, []string{"grimoire"})
		if err == nil {
			t.Fatal("runStatus() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "status request failed") {
			t.Fatalf("runStatus() error = %q, want wrapped request failure", err)
		}
	})

	if output != "" {
		t.Fatalf("runStatus() output = %q, want empty output on failure", output)
	}
}

func TestRunSyncFailureReturnsError(t *testing.T) {
	restore := stubDialAgent(t, fakeSextonClient{
		syncErr: status.Error(codes.FailedPrecondition, "agent is snoozed"),
	}, nil)
	defer restore()

	output := captureStdout(t, func() {
		err := runSync(nil, []string{"repo"})
		if err == nil {
			t.Fatal("runSync() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "sync request failed") {
			t.Fatalf("runSync() error = %q, want wrapped request failure", err)
		}
	})

	if output != "" {
		t.Fatalf("runSync() output = %q, want empty output on failure", output)
	}
}

func TestRunResumeSuccess(t *testing.T) {
	restore := stubDialAgent(t, fakeSextonClient{
		resumeResp: &sextonv1.ResumeResponse{Message: "resumed"},
	}, nil)
	defer restore()

	output := captureStdout(t, func() {
		if err := runResume(nil, []string{"repo"}); err != nil {
			t.Fatalf("runResume() error = %v", err)
		}
	})

	if output != "resumed\n" {
		t.Fatalf("runResume() output = %q, want %q", output, "resumed\n")
	}
}

func TestRunResumeFailureReturnsError(t *testing.T) {
	restore := stubDialAgent(t, fakeSextonClient{
		resumeErr: status.Error(codes.NotFound, `repo not found: "missing"`),
	}, nil)
	defer restore()

	output := captureStdout(t, func() {
		err := runResume(nil, []string{"missing"})
		if err == nil {
			t.Fatal("runResume() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "resume request failed") {
			t.Fatalf("runResume() error = %q, want wrapped request failure", err)
		}
	})

	if output != "" {
		t.Fatalf("runResume() output = %q, want empty output on failure", output)
	}
}

func TestRunSnoozeSuccess(t *testing.T) {
	restore := stubDialAgent(t, fakeSextonClient{
		snoozeResp: &sextonv1.SnoozeResponse{Expires: "2026-03-18T18:00:00Z"},
	}, nil)
	defer restore()

	output := captureStdout(t, func() {
		if err := runSnooze(nil, []string{"repo", "1h"}); err != nil {
			t.Fatalf("runSnooze() error = %v", err)
		}
	})

	if output != "snoozed until 2026-03-18T18:00:00Z\n" {
		t.Fatalf("runSnooze() output = %q, want %q", output, "snoozed until 2026-03-18T18:00:00Z\n")
	}
}

func TestRunSnoozeFailureReturnsError(t *testing.T) {
	restore := stubDialAgent(t, fakeSextonClient{
		snoozeErr: status.Error(codes.InvalidArgument, "invalid snooze duration"),
	}, nil)
	defer restore()

	output := captureStdout(t, func() {
		err := runSnooze(nil, []string{"repo", "later"})
		if err == nil {
			t.Fatal("runSnooze() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "snooze request failed") {
			t.Fatalf("runSnooze() error = %q, want wrapped request failure", err)
		}
	})

	if output != "" {
		t.Fatalf("runSnooze() output = %q, want empty output on failure", output)
	}
}

func stubDialAgent(t *testing.T, client sextonv1.SextonClient, err error) func() {
	t.Helper()
	prev := dialAgentFn
	dialAgentFn = func() (sextonv1.SextonClient, agentConn, error) {
		if err != nil {
			return nil, nil, err
		}
		return client, fakeAgentConn{}, nil
	}
	return func() {
		dialAgentFn = prev
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = prev

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy() error = %v", err)
	}
	_ = r.Close()

	return buf.String()
}
