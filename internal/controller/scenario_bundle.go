package controller

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const swarmConfigPartBytes = 450 * 1024

// writeCompressedScenarioParts compresses a resolved scenario and splits the
// compressed byte stream below Docker Swarm's per-config size limit.
func writeCompressedScenarioParts(sourcePath, outputDir string) ([]string, error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("open resolved scenario: %w", err)
	}
	defer source.Close()

	compressedPath := filepath.Join(outputDir, "scenario.yaml.gz")
	compressed, err := os.Create(compressedPath)
	if err != nil {
		return nil, fmt.Errorf("create compressed scenario: %w", err)
	}
	gzipWriter := gzip.NewWriter(compressed)
	_, copyErr := io.Copy(gzipWriter, source)
	gzipErr := gzipWriter.Close()
	closeErr := compressed.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if gzipErr != nil {
		return nil, gzipErr
	}
	if closeErr != nil {
		return nil, closeErr
	}

	data, err := os.ReadFile(compressedPath)
	if err != nil {
		return nil, fmt.Errorf("read compressed scenario: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("compressed scenario is empty")
	}
	parts := make([]string, 0, (len(data)+swarmConfigPartBytes-1)/swarmConfigPartBytes)
	for offset, index := 0, 0; offset < len(data); offset, index = offset+swarmConfigPartBytes, index+1 {
		end := min(offset+swarmConfigPartBytes, len(data))
		path := filepath.Join(outputDir, fmt.Sprintf("scenario.yaml.gz.part%03d", index))
		if err := os.WriteFile(path, data[offset:end], 0o644); err != nil {
			return nil, fmt.Errorf("write scenario config part: %w", err)
		}
		parts = append(parts, path)
	}
	return parts, nil
}

// MaterializeScenarioGzipParts reconstructs and decompresses comma-separated
// scenario parts mounted into the Swarm Controller container.
func MaterializeScenarioGzipParts(partsCSV string) (string, func(), error) {
	partPaths := make([]string, 0)
	for _, raw := range strings.Split(partsCSV, ",") {
		path := strings.TrimSpace(raw)
		if path != "" {
			partPaths = append(partPaths, path)
		}
	}
	if len(partPaths) == 0 {
		return "", func() {}, fmt.Errorf("no compressed scenario parts were provided")
	}

	files := make([]*os.File, 0, len(partPaths))
	readers := make([]io.Reader, 0, len(partPaths))
	closeFiles := func() {
		for _, file := range files {
			_ = file.Close()
		}
	}
	for _, path := range partPaths {
		file, err := os.Open(path)
		if err != nil {
			closeFiles()
			return "", func() {}, fmt.Errorf("open scenario part %s: %w", path, err)
		}
		files = append(files, file)
		readers = append(readers, file)
	}

	gzipReader, err := gzip.NewReader(io.MultiReader(readers...))
	if err != nil {
		closeFiles()
		return "", func() {}, fmt.Errorf("open compressed scenario stream: %w", err)
	}
	temporary, err := os.CreateTemp("", "peerkit-resolved-scenario-*.yaml")
	if err != nil {
		_ = gzipReader.Close()
		closeFiles()
		return "", func() {}, err
	}
	_, copyErr := io.Copy(temporary, gzipReader)
	gzipCloseErr := gzipReader.Close()
	fileCloseErr := temporary.Close()
	closeFiles()
	if copyErr != nil || gzipCloseErr != nil || fileCloseErr != nil {
		_ = os.Remove(temporary.Name())
		if copyErr != nil {
			return "", func() {}, copyErr
		}
		if gzipCloseErr != nil {
			return "", func() {}, gzipCloseErr
		}
		return "", func() {}, fileCloseErr
	}
	cleanup := func() { _ = os.Remove(temporary.Name()) }
	return temporary.Name(), cleanup, nil
}
