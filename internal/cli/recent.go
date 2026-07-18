package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const recentRunFileName = ".peerkit-last-run"

func recentRunStatePath(projectRoot string) (string, error) {
	if override := strings.TrimSpace(os.Getenv("PEERKIT_RECENT_RUN_FILE")); override != "" {
		path, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("resolve PEERKIT_RECENT_RUN_FILE: %w", err)
		}
		return path, nil
	}
	if strings.TrimSpace(projectRoot) == "" {
		projectRoot = "."
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	return filepath.Join(root, "bin", recentRunFileName), nil
}

func saveRecentRun(projectRoot, runDir string) error {
	statePath, err := recentRunStatePath(projectRoot)
	if err != nil {
		return err
	}
	absoluteRunDir, err := filepath.Abs(runDir)
	if err != nil {
		return fmt.Errorf("resolve run directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return fmt.Errorf("create recent-run state directory: %w", err)
	}
	temporary := statePath + ".tmp"
	if err := os.WriteFile(temporary, []byte(absoluteRunDir+"\n"), 0o644); err != nil {
		return fmt.Errorf("write recent-run state: %w", err)
	}
	if err := os.Rename(temporary, statePath); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit recent-run state: %w", err)
	}
	return nil
}

func loadRecentRun(projectRoot string) (string, error) {
	statePath, err := recentRunStatePath(projectRoot)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no recent run is recorded in %s; provide a run directory explicitly", statePath)
		}
		return "", fmt.Errorf("read recent-run state %s: %w", statePath, err)
	}
	runDir := strings.TrimSpace(string(data))
	if runDir == "" {
		return "", fmt.Errorf("recent-run state %s is empty", statePath)
	}
	if _, err := os.Stat(filepath.Join(runDir, "run.yaml")); err != nil {
		return "", fmt.Errorf("recent run %s is not usable: %w", runDir, err)
	}
	return runDir, nil
}

func clearRecentRunIfMatches(projectRoot, runDir string) error {
	statePath, err := recentRunStatePath(projectRoot)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	stored := strings.TrimSpace(string(data))
	storedAbs, _ := filepath.Abs(stored)
	runAbs, _ := filepath.Abs(runDir)
	if storedAbs == runAbs {
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
