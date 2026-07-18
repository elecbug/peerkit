package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecentRunRoundTrip(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".peerkit", "runs", "test")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "run.yaml"), []byte("project_name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveRecentRun(root, runDir); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadRecentRun(root)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(runDir)
	if loaded != want {
		t.Fatalf("loaded %q, want %q", loaded, want)
	}
	if err := clearRecentRunIfMatches(root, runDir); err != nil {
		t.Fatal(err)
	}
	statePath, err := recentRunStatePath(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file still exists: %v", err)
	}
}
