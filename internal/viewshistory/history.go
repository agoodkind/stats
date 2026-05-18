// Package viewshistory persists per-repo daily traffic counts so the
// Overview SVG can show a lifetime view total instead of GitHub's
// rolling 14-day window.
package viewshistory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
)

// History is repo full-name -> ISO date string (YYYY-MM-DD) -> view count,
// plus a one-time Seed offset folded into the total so the displayed view
// count can pick up a baseline from a prior external counter (e.g. the
// profile-README komarev badge) instead of starting at zero.
type History struct {
	Seed  int                       `json:"seed"`
	Repos map[string]map[string]int `json:"repos"`
}

// Load reads a History from path. If the file does not exist an empty
// history is returned without error.
func Load(path string) (History, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return History{Repos: map[string]map[string]int{}}, nil
		}
		slog.Error("read views history", "path", path, "error", err)
		return History{}, fmt.Errorf("read views history %q: %w", path, err)
	}
	var history History
	if err := json.Unmarshal(data, &history); err != nil {
		slog.Error("decode views history", "path", path, "error", err)
		return History{}, fmt.Errorf("decode views history %q: %w", path, err)
	}
	if history.Repos == nil {
		history.Repos = map[string]map[string]int{}
	}
	return history, nil
}

// Save writes history to path with deterministic indentation so the file
// produces stable diffs in git.
func Save(path string, history History) error {
	if history.Repos == nil {
		history.Repos = map[string]map[string]int{}
	}
	encoded, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		slog.Error("encode views history", "path", path, "error", err)
		return fmt.Errorf("encode views history: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		slog.Error("write views history", "path", path, "error", err)
		return fmt.Errorf("write views history %q: %w", path, err)
	}
	return nil
}

// Merge folds the latest fresh-from-GitHub daily counts into history.
// Each fresh count for a date overwrites the stored one (GitHub may revise
// recent counts as more data flows in).
func Merge(history History, fresh map[string]map[string]int) History {
	if history.Repos == nil {
		history.Repos = map[string]map[string]int{}
	}
	for repo, days := range fresh {
		existing := history.Repos[repo]
		if existing == nil {
			existing = map[string]int{}
		}
		maps.Copy(existing, days)
		history.Repos[repo] = existing
	}
	return history
}

// Total sums the seed offset plus every recorded count across every repo
// and date.
func Total(history History) int {
	total := history.Seed
	for _, days := range history.Repos {
		for _, count := range days {
			total += count
		}
	}
	return total
}
