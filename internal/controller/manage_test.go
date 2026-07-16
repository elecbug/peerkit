package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyCollectedResults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "messages.csv"), []byte("message_id\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCollectedResults(dir); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCollectedResultsRejectsMissingFile(t *testing.T) {
	if err := verifyCollectedResults(t.TempDir()); err == nil {
		t.Fatal("expected missing results to fail")
	}
}
