package viewshistory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMigratesSeedToCounter(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "views_history.json")
	historyJSON := `{
  "seed": 1152,
  "repos": {
    "agoodkind/stats": {
      "2026-05-24": 7
    }
  }
}
`
	if err := os.WriteFile(historyPath, []byte(historyJSON), 0o600); err != nil {
		t.Fatalf("write history fixture: %v", err)
	}

	history, err := Load(historyPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if history.Counter != 1152 {
		t.Fatalf("expected migrated counter 1152, got %d", history.Counter)
	}
	if history.Seed != 0 {
		t.Fatalf("expected seed to be cleared after migration, got %d", history.Seed)
	}
	if Total(history) != 1159 {
		t.Fatalf("expected total 1159, got %d", Total(history))
	}
}

func TestSaveWritesCounterOnly(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "views_history.json")
	history := History{
		Counter: 1159,
		Seed:    1152,
		Repos: map[string]map[string]int{
			"agoodkind/stats": {"2026-05-24": 7},
		},
	}

	if err := Save(historyPath, history); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	savedBytes, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read saved history: %v", err)
	}
	saved := string(savedBytes)
	if !strings.Contains(saved, `"counter": 1159`) {
		t.Fatalf("expected saved counter, got %s", saved)
	}
	if strings.Contains(saved, `"seed"`) {
		t.Fatalf("expected seed to be omitted, got %s", saved)
	}
}
