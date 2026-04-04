package config

import "testing"

func TestResolveHoldoutWindowsNormalizesAndMerges(t *testing.T) {
	windows, err := resolveHoldoutWindows(nil, []*HoldoutWindowEntry{
		{Start: "23:00", End: "01:00"},
		{Start: "10:00", End: "11:00"},
		{Start: "11:00", End: "12:30"},
		{Start: "00:30", End: "02:00"},
	}, nil)
	if err != nil {
		t.Fatalf("resolveHoldoutWindows() error = %v", err)
	}

	if len(windows) != 3 {
		t.Fatalf("resolveHoldoutWindows() windows = %d, want 3", len(windows))
	}

	if got := windows[0]; got.StartMinute != 0 || got.EndMinute != 120 {
		t.Fatalf("window[0] = %+v, want 00:00-02:00", got)
	}
	if got := windows[1]; got.StartMinute != 600 || got.EndMinute != 750 {
		t.Fatalf("window[1] = %+v, want 10:00-12:30", got)
	}
	if got := windows[2]; got.StartMinute != 1380 || got.EndMinute != 1440 {
		t.Fatalf("window[2] = %+v, want 23:00-24:00", got)
	}
}

func TestResolveHoldoutWindowsUsesLocalThenEntryThenDefaults(t *testing.T) {
	defaults := []*HoldoutWindowEntry{{Start: "01:00", End: "02:00"}}
	entry := []*HoldoutWindowEntry{{Start: "03:00", End: "04:00"}}
	local := []*HoldoutWindowEntry{{Start: "05:00", End: "06:00"}}

	windows, err := resolveHoldoutWindows(defaults, entry, local)
	if err != nil {
		t.Fatalf("resolveHoldoutWindows() error = %v", err)
	}

	if len(windows) != 1 || windows[0].StartMinute != 300 || windows[0].EndMinute != 360 {
		t.Fatalf("resolveHoldoutWindows() = %+v, want local window only", windows)
	}

	windows, err = resolveHoldoutWindows(defaults, entry, nil)
	if err != nil {
		t.Fatalf("resolveHoldoutWindows() error = %v", err)
	}
	if len(windows) != 1 || windows[0].StartMinute != 180 || windows[0].EndMinute != 240 {
		t.Fatalf("resolveHoldoutWindows() = %+v, want entry window only", windows)
	}

	windows, err = resolveHoldoutWindows(defaults, nil, nil)
	if err != nil {
		t.Fatalf("resolveHoldoutWindows() error = %v", err)
	}
	if len(windows) != 1 || windows[0].StartMinute != 60 || windows[0].EndMinute != 120 {
		t.Fatalf("resolveHoldoutWindows() = %+v, want defaults window only", windows)
	}
}

func TestResolveHoldoutWindowsRejectsInvalidTime(t *testing.T) {
	_, err := resolveHoldoutWindows(nil, []*HoldoutWindowEntry{
		{Start: "25:00", End: "02:00"},
	}, nil)
	if err == nil {
		t.Fatal("resolveHoldoutWindows() error = nil, want invalid time")
	}
}

func TestResolveHoldoutWindowsRejectsZeroLengthWindow(t *testing.T) {
	_, err := resolveHoldoutWindows(nil, []*HoldoutWindowEntry{
		{Start: "02:00", End: "02:00"},
	}, nil)
	if err == nil {
		t.Fatal("resolveHoldoutWindows() error = nil, want zero-length error")
	}
}

func TestResolveIncludesHoldoutWindows(t *testing.T) {
	repoRoot := t.TempDir()

	resolved, err := Resolve(
		&RepoEntry{
			Path: repoRoot,
			HoldoutWindows: []*HoldoutWindowEntry{
				{Start: "09:00", End: "10:00"},
			},
		},
		&RepoDefaults{},
		&RepoLocalConfig{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if len(resolved.HoldoutWindows) != 1 {
		t.Fatalf("Resolve() holdout windows = %d, want 1", len(resolved.HoldoutWindows))
	}
	if got := resolved.HoldoutWindows[0]; got.StartMinute != 540 || got.EndMinute != 600 {
		t.Fatalf("Resolve() holdout window = %+v, want 09:00-10:00", got)
	}
}
