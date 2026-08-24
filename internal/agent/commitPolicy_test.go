package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/michaelquigley/sexton/internal/config"
	"github.com/michaelquigley/sexton/internal/git"
	"github.com/michaelquigley/sexton/internal/llm"
)

func statusWithEntries(entries ...git.StatusEntry) *git.Status {
	status := git.NewStatus()
	status.Entries = append(status.Entries, entries...)
	for _, entry := range entries {
		switch {
		case entry.X == '?' && entry.Y == '?':
			status.Untracked = append(status.Untracked, entry.Path)
		case entry.X == 'A' || entry.Y == 'A':
			status.Added = append(status.Added, entry.Path)
		case entry.X == 'D' || entry.Y == 'D':
			status.Deleted = append(status.Deleted, entry.Path)
		default:
			status.Modified = append(status.Modified, entry.Path)
		}
	}
	return status
}

func messagesWithSeverity(events []AlertEvent, severity string) []string {
	var messages []string
	for _, event := range events {
		if event.Severity == severity {
			messages = append(messages, event.Message)
		}
	}
	return messages
}

func TestPartitionStatusClassifiesRenameEndpointsAndIgnoresCopySource(t *testing.T) {
	status := statusWithEntries(
		git.StatusEntry{Path: "journal/entry.md", X: ' ', Y: 'M'},
		git.StatusEntry{Path: "drafts/new.md", X: '?', Y: '?'},
		git.StatusEntry{OldPath: "journal/old.md", Path: "drafts/moved.md", X: 'R', Y: ' '},
		git.StatusEntry{OldPath: "archive/source.md", Path: "journal/copy.md", X: 'C', Y: ' '},
	)

	partition := partitionStatus(status, config.PolicyRegions, []string{"journal/", "literal[1]/"})
	if !partition.hasSelectedChanges {
		t.Fatal("expected an in-region partition")
	}
	if !reflect.DeepEqual(partition.regions, []string{"journal/"}) {
		t.Fatalf("regions = %#v, want journal region only", partition.regions)
	}
	if !reflect.DeepEqual(partition.unselectedPaths, []string{"drafts/moved.md", "drafts/new.md"}) {
		t.Fatalf("unselected paths = %#v", partition.unselectedPaths)
	}
	if !partition.hasTrackedUnselected {
		t.Fatal("expected the out-of-region rename endpoint to pause pulls")
	}

	none := partitionStatus(status, config.PolicyNone, nil)
	wantNone := []string{"drafts/moved.md", "drafts/new.md", "journal/copy.md", "journal/entry.md", "journal/old.md"}
	if !reflect.DeepEqual(none.unselectedPaths, wantNone) {
		t.Fatalf("none unselected paths = %#v, want %#v", none.unselectedPaths, wantNone)
	}
	if containsString(none.unselectedPaths, "archive/source.md") {
		t.Fatal("copy source was treated as a changed endpoint")
	}

	all := partitionStatus(status, config.PolicyAll, []string{"journal/"})
	if !all.hasSelectedChanges || len(all.unselectedPaths) != 0 || all.hasTrackedUnselected {
		t.Fatalf("all partition = %#v", all)
	}
}

func TestFormatAttentionDetailIsInjectiveAndCapped(t *testing.T) {
	if got := formatAttentionDetail([]string{"plain.md"}, false); got != `"plain.md"` {
		t.Fatalf("detail = %q, want an unconditionally quoted path", got)
	}
	if formatAttentionDetail([]string{"a, b"}, false) == formatAttentionDetail([]string{"a", "b"}, false) {
		t.Fatal("delimiter-like filename collided with two filenames")
	}
	if formatAttentionDetail([]string{"foo (pulls paused)"}, false) == formatAttentionDetail([]string{"foo"}, true) {
		t.Fatal("filename collided with pulls-paused framing")
	}
	invalidDetail := formatAttentionDetail([]string{string([]byte{'x', 0xff})}, false)
	if !utf8.ValidString(invalidDetail) || !strings.Contains(invalidDetail, `\xff`) {
		t.Fatalf("invalid UTF-8 was not rendered safely: %q", invalidDetail)
	}

	paths := make([]string, 12)
	for i := range paths {
		paths[i] = string(rune('a'+i)) + ".md"
	}
	detail := formatAttentionDetail(paths, true)
	if !strings.Contains(detail, `"a.md"`) || !strings.Contains(detail, `"j.md"`) {
		t.Fatalf("detail does not contain the sorted visible paths: %q", detail)
	}
	if strings.Contains(detail, `"k.md"`) || !strings.HasSuffix(detail, ", +2 more (pulls paused)") {
		t.Fatalf("detail cap or suffix = %q", detail)
	}
}

func TestSyncRegionsCommitsExactPartitionAndRaisesAttention(t *testing.T) {
	workingStatus := statusWithEntries(
		git.StatusEntry{Path: "journal/entry.md", X: ' ', Y: 'M'},
		git.StatusEntry{Path: "drafts/wip.md", X: ' ', Y: 'M'},
		git.StatusEntry{Path: "scratch.tmp", X: '?', Y: '?'},
	)
	committedStatus := modifiedStatus("journal/entry.md")
	g := &stubGit{
		status:         workingStatus,
		showNameStatus: committedStatus,
		shortHEAD:      "terminal-head",
	}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)
	a.cfg.CommitPolicy = config.PolicyRegions
	a.cfg.CommitRegions = []string{"journal/", "notes[1]/"}

	a.sync()

	if !reflect.DeepEqual(g.stageRegions, [][]string{{"journal/"}}) {
		t.Fatalf("stage regions = %#v", g.stageRegions)
	}
	if g.commitOnlyCalls != 1 || !reflect.DeepEqual(g.commitRegions, [][]string{{"journal/"}}) {
		t.Fatalf("commit-only calls = %d, regions = %#v", g.commitOnlyCalls, g.commitRegions)
	}
	if !reflect.DeepEqual(g.commitMessages, []string{placeholderCommitMessage}) {
		t.Fatalf("placeholder messages = %#v", g.commitMessages)
	}
	if g.rewordCalls != 1 || g.rewordOldSHA != "created-sha" || g.rewordMessage != "sexton: update 1 file" {
		t.Fatalf("reword = calls %d, old %q, message %q", g.rewordCalls, g.rewordOldSHA, g.rewordMessage)
	}
	if g.pullCalls != 0 || g.pushCalls != 1 {
		t.Fatalf("pull calls = %d, push calls = %d", g.pullCalls, g.pushCalls)
	}
	if got := a.State(); got != Attention {
		t.Fatalf("state = %s, want attention", got)
	}
	wantDetail := `"drafts/wip.md", "scratch.tmp" (pulls paused)`
	if got := a.AttentionDetail(); got != wantDetail {
		t.Fatalf("attention detail = %q, want %q", got, wantDetail)
	}
	if got := a.LastCommit(); got != "terminal-head" {
		t.Fatalf("last commit = %q, want terminal HEAD", got)
	}
	if len(alerts.events) != 2 {
		t.Fatalf("alerts = %#v", alerts.events)
	}
	if alerts.events[0].Severity != "attention" || alerts.events[0].Message != wantDetail {
		t.Fatalf("attention alert = %#v", alerts.events[0])
	}
	complete := alerts.events[1]
	if complete.Message != "sync complete" || strings.Contains(complete.Message, "terminal-head") {
		t.Fatalf("completion message = %q", complete.Message)
	}
	if complete.Files == nil || !reflect.DeepEqual(complete.Files.Modified, []string{"journal/entry.md"}) {
		t.Fatalf("completion files = %#v", complete.Files)
	}
}

func TestSyncRegionsPullsAroundUntrackedOutOfRegionDirt(t *testing.T) {
	g := &stubGit{
		status:    statusWithEntries(git.StatusEntry{Path: "draft.tmp", X: '?', Y: '?'}),
		shortHEAD: "abc123",
	}
	a := newAgentForTest(g, nil)
	a.cfg.CommitPolicy = config.PolicyRegions
	a.cfg.CommitRegions = []string{"journal/"}

	a.sync()

	if g.commitCalls != 0 || g.pullCalls != 1 || g.pushCalls != 1 {
		t.Fatalf("commit/pull/push calls = %d/%d/%d", g.commitCalls, g.pullCalls, g.pushCalls)
	}
	if got := a.AttentionDetail(); got != `"draft.tmp"` {
		t.Fatalf("attention detail = %q", got)
	}
}

func TestSyncNonePushesOperatorCommitsWithoutCreatingOne(t *testing.T) {
	g := &stubGit{status: git.NewStatus(), shortHEAD: "operator-head"}
	a := newAgentForTest(g, nil)
	a.cfg.CommitPolicy = config.PolicyNone

	a.sync()

	if g.stageCalls != 0 || g.commitCalls != 0 {
		t.Fatalf("stage calls = %d, commit calls = %d", g.stageCalls, g.commitCalls)
	}
	if g.pullCalls != 1 || g.pushCalls != 1 {
		t.Fatalf("pull calls = %d, push calls = %d", g.pullCalls, g.pushCalls)
	}
	if got := a.LastCommit(); got != "operator-head" {
		t.Fatalf("last commit = %q", got)
	}
}

func TestSyncAllUsesCommitFirstSequenceAndExactMetadata(t *testing.T) {
	g := &stubGit{
		status:         modifiedStatus("notes.md"),
		showNameStatus: statusWithEntries(git.StatusEntry{Path: "hook.md", X: 'A', Y: ' '}),
		shortHEAD:      "operator-after-reword",
	}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)

	a.sync()

	wantSequence := []string{"status", "stage-all", "commit", "show-name-status", "reword", "pull", "push"}
	if !reflect.DeepEqual(g.callSequence, wantSequence) {
		t.Fatalf("call sequence = %#v, want %#v", g.callSequence, wantSequence)
	}
	if g.rewordMessage != "sexton: add 1 file" {
		t.Fatalf("reword message = %q", g.rewordMessage)
	}
	if got := a.LastCommit(); got != "operator-after-reword" {
		t.Fatalf("last commit = %q", got)
	}
	if len(alerts.events) != 1 || alerts.events[0].Message != "sync complete" {
		t.Fatalf("completion alerts = %#v", alerts.events)
	}
	if alerts.events[0].Files == nil || !reflect.DeepEqual(alerts.events[0].Files.Added, []string{"hook.md"}) {
		t.Fatalf("completion metadata = %#v", alerts.events[0])
	}
}

func TestSyncRegionsPassesMetacharacterRegionLiterally(t *testing.T) {
	g := &stubGit{
		status:         modifiedStatus("notes[1]/entry.md"),
		showNameStatus: modifiedStatus("notes[1]/entry.md"),
	}
	a := newAgentForTest(g, nil)
	a.cfg.CommitPolicy = config.PolicyRegions
	a.cfg.CommitRegions = []string{"notes[1]/"}

	a.sync()

	want := [][]string{{"notes[1]/"}}
	if !reflect.DeepEqual(g.stageRegions, want) || !reflect.DeepEqual(g.commitRegions, want) {
		t.Fatalf("stage regions = %#v, commit regions = %#v", g.stageRegions, g.commitRegions)
	}
}

func TestSyncRegionsSplitsStagedRenameAcrossBoundary(t *testing.T) {
	g := &stubGit{
		status: statusWithEntries(git.StatusEntry{
			OldPath: "journal/old.md",
			Path:    "drafts/moved.md",
			X:       'R',
			Y:       ' ',
		}),
		showNameStatus: statusWithEntries(git.StatusEntry{Path: "journal/old.md", X: 'D', Y: ' '}),
	}
	a := newAgentForTest(g, nil)
	a.cfg.CommitPolicy = config.PolicyRegions
	a.cfg.CommitRegions = []string{"journal/"}

	a.sync()

	if !reflect.DeepEqual(g.commitRegions, [][]string{{"journal/"}}) {
		t.Fatalf("commit regions = %#v", g.commitRegions)
	}
	if got := a.AttentionDetail(); got != `"drafts/moved.md" (pulls paused)` {
		t.Fatalf("attention detail = %q", got)
	}
}

func TestMidCycleDirtyPullRaceRestsWithoutError(t *testing.T) {
	g := &stubGit{status: git.NewStatus(), pullErr: git.ErrDirtyWorkingTree}
	a := newAgentForTest(g, nil)

	a.sync()

	if got := a.State(); got != Watching {
		t.Fatalf("state = %s, want watching", got)
	}
	if got := a.ErrorDetail(); got != "" {
		t.Fatalf("error detail = %q", got)
	}
	if g.pushCalls != 0 {
		t.Fatalf("push calls = %d, want zero after incomplete pull", g.pushCalls)
	}
}

func TestSetAttentionDoesNotChangeMidCycleState(t *testing.T) {
	a := newAgentForTest(&stubGit{}, nil)
	a.state = Syncing
	a.setAttention(`"draft.md"`)
	if got := a.State(); got != Syncing {
		t.Fatalf("state = %s, want syncing", got)
	}
}

func TestAttentionDeduplicatesRealertsAndAnnouncesRecovery(t *testing.T) {
	g := &stubGit{status: modifiedStatus("drafts/a.md")}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)
	a.cfg.CommitPolicy = config.PolicyNone

	a.sync()
	a.sync()
	g.status = modifiedStatus("drafts/a.md", "drafts/b.md")
	a.sync()
	g.status = git.NewStatus()
	a.sync()

	wantAttention := []string{
		`"drafts/a.md" (pulls paused)`,
		`"drafts/a.md", "drafts/b.md" (pulls paused)`,
	}
	if got := messagesWithSeverity(alerts.events, "attention"); !reflect.DeepEqual(got, wantAttention) {
		t.Fatalf("attention alerts = %#v, want %#v", got, wantAttention)
	}
	if got := messagesWithSeverity(alerts.events, "info"); !reflect.DeepEqual(got, []string{"local changes resolved"}) {
		t.Fatalf("info alerts = %#v", got)
	}
	if got := a.State(); got != Watching || a.AttentionDetail() != "" {
		t.Fatalf("state/detail = %s/%q", got, a.AttentionDetail())
	}
}

func TestErrorOutranksAttentionAndRecoveryRevealsIt(t *testing.T) {
	g := &stubGit{
		status:  modifiedStatus("drafts/wip.md"),
		pushErr: errors.New("remote rejected push"),
	}
	alerts := &recordingAlerter{}
	a := newAgentForTest(g, alerts)
	a.cfg.CommitPolicy = config.PolicyNone

	a.sync()
	if got := a.State(); got != Error {
		t.Fatalf("state = %s, want error", got)
	}
	if a.AttentionDetail() == "" || a.ErrorDetail() == "" {
		t.Fatalf("standing details = attention %q, error %q", a.AttentionDetail(), a.ErrorDetail())
	}

	g.pushErr = nil
	a.sync()
	if got := a.State(); got != Attention {
		t.Fatalf("state after recovery = %s, want attention", got)
	}
	if a.ErrorDetail() != "" || a.AttentionDetail() == "" {
		t.Fatalf("details after recovery = attention %q, error %q", a.AttentionDetail(), a.ErrorDetail())
	}
	if got := messagesWithSeverity(alerts.events, "info"); !reflect.DeepEqual(got, []string{"recovered from error"}) {
		t.Fatalf("recovery alerts = %#v", got)
	}
}

func TestRestingTransitionsPreserveAttention(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 31, 0, 0, time.Local)

	t.Run("trigger over expired snooze", func(t *testing.T) {
		a := newAgentForTest(&stubGit{}, nil)
		a.now = func() time.Time { return now }
		a.state = Snoozed
		a.snoozeUntil = now.Add(-time.Minute)
		a.attentionDetail = `"draft.md"`
		if err := a.TriggerSync(); err != nil {
			t.Fatalf("TriggerSync() error = %v", err)
		}
		if got := a.State(); got != Attention {
			t.Fatalf("state = %s, want attention", got)
		}
	})

	t.Run("resume over expired snooze", func(t *testing.T) {
		a := newAgentForTest(&stubGit{}, nil)
		a.now = func() time.Time { return now }
		a.state = Snoozed
		a.snoozeUntil = now.Add(-time.Minute)
		a.attentionDetail = `"draft.md"`
		if _, err := a.Resume(); err != nil {
			t.Fatalf("Resume() error = %v", err)
		}
		if got := a.State(); got != Attention {
			t.Fatalf("state = %s, want attention", got)
		}
	})

	t.Run("snooze expiry", func(t *testing.T) {
		a := newAgentForTest(&stubGit{}, nil)
		a.now = func() time.Time { return now }
		a.state = Snoozed
		a.snoozeUntil = now
		a.attentionDetail = `"draft.md"`
		a.handleSnoozeExpiry()
		if got := a.State(); got != Attention {
			t.Fatalf("state = %s, want attention", got)
		}
	})

	t.Run("holdout exit", func(t *testing.T) {
		a := newAgentForTest(&stubGit{}, nil)
		a.now = func() time.Time { return now }
		a.cfg.HoldoutWindows = []*config.ResolvedHoldoutWindow{{StartMinute: 600, EndMinute: 630}}
		a.state = Holdout
		a.attentionDetail = `"draft.md"`
		a.handleHoldoutTransition()
		if got := a.State(); got != Attention {
			t.Fatalf("state = %s, want attention", got)
		}
	})

	t.Run("sync after holdout remains snoozed", func(t *testing.T) {
		g := &stubGit{}
		a := newAgentForTest(g, nil)
		a.now = func() time.Time { return now }
		a.state = Holdout
		a.snoozeUntil = now.Add(time.Minute)
		a.sync()
		if got := a.State(); got != Snoozed {
			t.Fatalf("state = %s, want snoozed", got)
		}
		if len(g.callSequence) != 0 {
			t.Fatalf("snoozed sync touched git: %#v", g.callSequence)
		}
	})
}

type callbackAlerter struct {
	events   []AlertEvent
	callback func(AlertEvent)
}

func (a *callbackAlerter) Alert(_ context.Context, event AlertEvent) error {
	a.events = append(a.events, event)
	if a.callback != nil {
		a.callback(event)
	}
	return nil
}

func TestSnoozeFromAttentionAlertStopsBeforeRemote(t *testing.T) {
	g := &stubGit{status: statusWithEntries(git.StatusEntry{Path: "draft.tmp", X: '?', Y: '?'})}
	alerts := &callbackAlerter{}
	a := newAgentForTest(g, alerts)
	a.cfg.CommitPolicy = config.PolicyNone
	alerts.callback = func(event AlertEvent) {
		if event.Severity == "attention" {
			_, _ = a.Snooze(time.Minute)
		}
	}

	a.sync()

	if got := a.State(); got != Snoozed {
		t.Fatalf("state = %s, want snoozed", got)
	}
	if g.pullCalls != 0 || g.pushCalls != 0 {
		t.Fatalf("remote calls after snooze = pull %d, push %d", g.pullCalls, g.pushCalls)
	}
	a.mu.Lock()
	a.clearSnoozeLocked()
	a.mu.Unlock()
}

func TestSnoozeDuringFailingPushRetainsErrorBeneathPause(t *testing.T) {
	reachedPush := make(chan struct{})
	releasePush := make(chan struct{})
	g := &stubGit{
		status:  git.NewStatus(),
		pushErr: errors.New("push failed hard"),
		onPush: func(context.Context) {
			close(reachedPush)
			<-releasePush
		},
	}
	a := newAgentForTest(g, nil)
	done := make(chan struct{})
	go func() {
		a.sync()
		close(done)
	}()
	<-reachedPush
	if _, err := a.Snooze(time.Minute); err != nil {
		t.Fatalf("Snooze() error = %v", err)
	}
	close(releasePush)
	<-done

	if got := a.State(); got != Snoozed {
		t.Fatalf("state = %s, want snoozed", got)
	}
	if got := a.ErrorDetail(); !strings.Contains(got, "push failed hard") {
		t.Fatalf("error detail = %q", got)
	}
	beforePull, beforePush := g.pullCalls, g.pushCalls
	a.sync()
	if g.pullCalls != beforePull || g.pushCalls != beforePush {
		t.Fatalf("snoozed poll touched remote: pull %d->%d, push %d->%d", beforePull, g.pullCalls, beforePush, g.pushCalls)
	}
	a.mu.Lock()
	a.clearSnoozeLocked()
	a.mu.Unlock()
}

func TestSnoozeThatExpiresDuringPushDoesNotPauseOrRestart(t *testing.T) {
	reachedPush := make(chan struct{})
	releasePush := make(chan struct{})
	clock := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	g := &stubGit{
		status: git.NewStatus(),
		onPush: func(context.Context) {
			close(reachedPush)
			<-releasePush
		},
	}
	a := newAgentForTest(g, nil)
	a.now = func() time.Time { return clock }
	done := make(chan struct{})
	go func() {
		a.sync()
		close(done)
	}()
	<-reachedPush
	if _, err := a.Snooze(time.Minute); err != nil {
		t.Fatalf("Snooze() error = %v", err)
	}
	clock = clock.Add(2 * time.Minute)
	close(releasePush)
	<-done

	if got := a.State(); got != Watching {
		t.Fatalf("state = %s, want watching", got)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.snoozeUntil.IsZero() || a.snoozeTimer != nil || a.snoozePending {
		t.Fatalf("expired snooze was retained or restarted: until %v, timer %v, pending %t", a.snoozeUntil, a.snoozeTimer, a.snoozePending)
	}
}

func TestCompleteSyncUsesPausePrecedence(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	a := newAgentForTest(&stubGit{}, nil)
	a.now = func() time.Time { return now }
	a.state = Syncing
	a.attentionDetail = `"draft.md"`
	a.snoozeUntil = now.Add(time.Minute)
	a.snoozePending = true
	a.completeSync("abc123", now)
	if got := a.State(); got != Snoozed {
		t.Fatalf("state = %s, want snoozed over attention", got)
	}

	b := newAgentForTest(&stubGit{}, nil)
	b.now = func() time.Time { return now }
	b.cfg.HoldoutWindows = []*config.ResolvedHoldoutWindow{{StartMinute: 720, EndMinute: 780}}
	b.state = Syncing
	b.attentionDetail = `"draft.md"`
	b.completeSync("abc123", now)
	if got := b.State(); got != Holdout {
		t.Fatalf("state = %s, want holdout over attention", got)
	}
}

func TestRealGitCommitMetadataStaysBoundedDuringLLMEdit(t *testing.T) {
	dir := initAgentGitRepo(t)
	journalPath := filepath.Join(dir, "journal", "entry.md")
	draftPath := filepath.Join(dir, "drafts", "wip.md")
	writeAgentTestFile(t, journalPath, "base journal\n")
	writeAgentTestFile(t, draftPath, "base draft\n")
	runAgentGit(t, dir, "add", "-A")
	runAgentGit(t, dir, "commit", "-m", "base")
	writeAgentTestFile(t, journalPath, "committed version\n")
	writeAgentTestFile(t, draftPath, "standing draft\n")

	requestBodies := make(chan string, 1)
	mutationErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			requestBodies <- string(body)
		} else {
			requestBodies <- ""
		}
		mutationErrors <- os.WriteFile(journalPath, []byte("next-cycle version\n"), 0o644)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"summarize exact committed journal edit"}}]}`)
	}))
	defer server.Close()

	cfg := &config.ResolvedRepo{
		Path:                dir,
		Name:                "real-git",
		PollInterval:        time.Second,
		Branch:              "main",
		Remote:              "origin",
		CommitPolicy:        config.PolicyRegions,
		CommitRegions:       []string{"journal/"},
		CommitMessagePrompt: config.DefaultCommitMessagePrompt,
		Hooks:               &config.ResolvedHooks{},
	}
	g := git.New(dir, "")
	if g == nil {
		t.Fatal("git.New() returned nil")
	}
	alerts := &recordingAlerter{}
	a := NewAgent(cfg, g)
	a.alerter = alerts
	a.llm = llm.NewClient(&config.LLMConfig{Endpoint: server.URL, Model: "test"})

	a.sync()

	if err := <-mutationErrors; err != nil {
		t.Fatalf("mutate during LLM request: %v", err)
	}
	requestBody := <-requestBodies
	if !strings.Contains(requestBody, "committed version") || strings.Contains(requestBody, "next-cycle version") || strings.Contains(requestBody, "standing draft") {
		t.Fatalf("LLM request was not bounded to the created commit: %s", requestBody)
	}
	if got := strings.TrimSpace(runAgentGit(t, dir, "show", "HEAD:journal/entry.md")); got != "committed version" {
		t.Fatalf("committed journal content = %q", got)
	}
	if got := strings.TrimSpace(runAgentGit(t, dir, "log", "-1", "--format=%s")); got != "summarize exact committed journal edit" {
		t.Fatalf("commit message = %q", got)
	}
	working, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read working file: %v", err)
	}
	if string(working) != "next-cycle version\n" {
		t.Fatalf("working content = %q", working)
	}
	if got := a.State(); got != Attention {
		t.Fatalf("state = %s, want attention from standing draft", got)
	}
	if len(alerts.events) != 2 || alerts.events[1].Message != "sync complete" {
		t.Fatalf("alerts = %#v", alerts.events)
	}
	complete := alerts.events[1]
	if complete.CommitMessage != "summarize exact committed journal edit" || complete.Files == nil || !reflect.DeepEqual(complete.Files.Modified, []string{"journal/entry.md"}) {
		t.Fatalf("completion metadata = %#v", complete)
	}
}

func TestRealGitPreCommitAdditionShapesFallbackFromCreatedCommit(t *testing.T) {
	dir := initAgentGitRepo(t)
	journalPath := filepath.Join(dir, "journal", "entry.md")
	writeAgentTestFile(t, journalPath, "base\n")
	runAgentGit(t, dir, "add", "-A")
	runAgentGit(t, dir, "commit", "-m", "base")
	writeAgentTestFile(t, journalPath, "changed\n")

	cfg := &config.ResolvedRepo{
		Path:          dir,
		Name:          "real-hook",
		PollInterval:  time.Second,
		Branch:        "main",
		Remote:        "origin",
		CommitPolicy:  config.PolicyRegions,
		CommitRegions: []string{"journal/"},
		Hooks: &config.ResolvedHooks{
			PreCommit: []*config.ResolvedHook{{
				Command: "printf 'hook file\\n' > journal/hook.md",
				Dir:     dir,
				Timeout: time.Second,
			}},
		},
	}
	g := git.New(dir, "")
	alerts := &recordingAlerter{}
	a := NewAgent(cfg, g)
	a.alerter = alerts

	a.sync()

	if got := strings.TrimSpace(runAgentGit(t, dir, "log", "-1", "--format=%s")); got != "sexton: add 1 file, update 1 file" {
		t.Fatalf("fallback message = %q", got)
	}
	if got := strings.TrimSpace(runAgentGit(t, dir, "show", "HEAD:journal/hook.md")); got != "hook file" {
		t.Fatalf("hook file content = %q", got)
	}
	if len(alerts.events) != 1 || alerts.events[0].Files == nil {
		t.Fatalf("alerts = %#v", alerts.events)
	}
	if !reflect.DeepEqual(alerts.events[0].Files.Added, []string{"journal/hook.md"}) || !reflect.DeepEqual(alerts.events[0].Files.Modified, []string{"journal/entry.md"}) {
		t.Fatalf("completion files = %#v", alerts.events[0].Files)
	}
}

func TestRealGitPartialCommitExcludesNewlyStagedOutsideRegion(t *testing.T) {
	dir := initAgentGitRepo(t)
	journalPath := filepath.Join(dir, "journal", "entry.md")
	draftPath := filepath.Join(dir, "drafts", "wip.md")
	writeAgentTestFile(t, journalPath, "base journal\n")
	writeAgentTestFile(t, draftPath, "base draft\n")
	runAgentGit(t, dir, "add", "-A")
	runAgentGit(t, dir, "commit", "-m", "base")
	writeAgentTestFile(t, journalPath, "changed journal\n")
	writeAgentTestFile(t, draftPath, "staged by hook\n")

	cfg := &config.ResolvedRepo{
		Path:          dir,
		Name:          "partial-boundary",
		PollInterval:  time.Second,
		Branch:        "main",
		Remote:        "origin",
		CommitPolicy:  config.PolicyRegions,
		CommitRegions: []string{"journal/"},
		Hooks: &config.ResolvedHooks{
			PreCommit: []*config.ResolvedHook{{
				Command: "git add -- drafts/wip.md",
				Dir:     dir,
				Timeout: time.Second,
			}},
		},
	}
	g := git.New(dir, "")
	a := NewAgent(cfg, g)
	a.alerter = &recordingAlerter{}

	a.sync()

	if got := strings.TrimSpace(runAgentGit(t, dir, "show", "HEAD:drafts/wip.md")); got != "base draft" {
		t.Fatalf("out-of-region content entered commit: %q", got)
	}
	if got := strings.TrimSpace(runAgentGit(t, dir, "show", "HEAD:journal/entry.md")); got != "changed journal" {
		t.Fatalf("in-region content missing from commit: %q", got)
	}
	status := runAgentGit(t, dir, "status", "--porcelain")
	if !strings.Contains(status, "M  drafts/wip.md") {
		t.Fatalf("out-of-region staged work was not preserved: %q", status)
	}
}

func initAgentGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runAgentGit(t, dir, "init", "-b", "main")
	runAgentGit(t, dir, "config", "user.email", "sexton-test@example.com")
	runAgentGit(t, dir, "config", "user.name", "Sexton Test")
	runAgentGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func writeAgentTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runAgentGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func containsString(values []string, want string) bool {
	index := sort.SearchStrings(values, want)
	return index < len(values) && values[index] == want
}
