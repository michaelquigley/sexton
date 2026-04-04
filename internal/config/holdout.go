package config

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const minutesPerDay = 24 * 60

func resolveHoldoutWindows(defaults, entry, local []*HoldoutWindowEntry) ([]*ResolvedHoldoutWindow, error) {
	var entries []*HoldoutWindowEntry
	switch {
	case len(local) > 0:
		entries = local
	case len(entry) > 0:
		entries = entry
	case len(defaults) > 0:
		entries = defaults
	default:
		return nil, nil
	}

	var windows []*ResolvedHoldoutWindow
	for _, entry := range entries {
		if entry == nil {
			return nil, fmt.Errorf("holdout window entry cannot be null")
		}

		startMinute, err := parseClockMinute(entry.Start)
		if err != nil {
			return nil, fmt.Errorf("invalid holdout window start %q: %w", entry.Start, err)
		}

		endMinute, err := parseClockMinute(entry.End)
		if err != nil {
			return nil, fmt.Errorf("invalid holdout window end %q: %w", entry.End, err)
		}

		if startMinute == endMinute {
			return nil, fmt.Errorf("holdout window start %q must differ from end %q", entry.Start, entry.End)
		}

		if endMinute > startMinute {
			windows = append(windows, &ResolvedHoldoutWindow{
				StartMinute: startMinute,
				EndMinute:   endMinute,
			})
			continue
		}

		windows = append(windows,
			&ResolvedHoldoutWindow{StartMinute: 0, EndMinute: endMinute},
			&ResolvedHoldoutWindow{StartMinute: startMinute, EndMinute: minutesPerDay},
		)
	}

	return mergeHoldoutWindows(windows), nil
}

func mergeHoldoutWindows(windows []*ResolvedHoldoutWindow) []*ResolvedHoldoutWindow {
	if len(windows) == 0 {
		return nil
	}

	sort.Slice(windows, func(i, j int) bool {
		if windows[i].StartMinute == windows[j].StartMinute {
			return windows[i].EndMinute < windows[j].EndMinute
		}
		return windows[i].StartMinute < windows[j].StartMinute
	})

	merged := []*ResolvedHoldoutWindow{{
		StartMinute: windows[0].StartMinute,
		EndMinute:   windows[0].EndMinute,
	}}

	for _, window := range windows[1:] {
		last := merged[len(merged)-1]
		if window.StartMinute <= last.EndMinute {
			if window.EndMinute > last.EndMinute {
				last.EndMinute = window.EndMinute
			}
			continue
		}
		merged = append(merged, &ResolvedHoldoutWindow{
			StartMinute: window.StartMinute,
			EndMinute:   window.EndMinute,
		})
	}

	return merged
}

func parseClockMinute(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("time cannot be empty")
	}

	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}

	return parsed.Hour()*60 + parsed.Minute(), nil
}
