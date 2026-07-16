package controller

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTarGZ(t *testing.T) {
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	content := []byte("hello\n")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "n000.jsonl", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	destination := t.TempDir()
	if err := extractTarGZ(bytes.NewReader(archive.Bytes()), destination); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(destination, "n000.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, content) {
		t.Fatalf("unexpected content %q", actual)
	}
}

func TestPushableImageReference(t *testing.T) {
	if isPushableImageReference("peerkit-peer:dev") {
		t.Fatal("unqualified local image must not be considered pushable")
	}
	if !isPushableImageReference("registry.local:5000/peerkit/peerkit:0.7.0") {
		t.Fatal("registry-qualified image should be pushable")
	}
}

func TestScenarioGzipPartsRoundTrip(t *testing.T) {
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "resolved-scenario.yaml")
	content := make([]byte, 1_200_000)
	rng := rand.New(rand.NewSource(42))
	if _, err := rng.Read(content); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	parts, err := writeCompressedScenarioParts(sourcePath, sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 {
		t.Fatalf("expected compressed scenario to be split, got %d part(s)", len(parts))
	}
	for _, part := range parts {
		info, err := os.Stat(part)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > swarmConfigPartBytes {
			t.Fatalf("config part exceeds limit: %d", info.Size())
		}
	}
	materialized, cleanup, err := MaterializeScenarioGzipParts(strings.Join(parts, ","))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	actual, err := os.ReadFile(materialized)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, content) {
		t.Fatalf("scenario round trip mismatch: got %d bytes want %d", len(actual), len(content))
	}
}
