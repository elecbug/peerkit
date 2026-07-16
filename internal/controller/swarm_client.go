package controller

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func waitSwarmRun(ctx context.Context, controllerURL string) (*SwarmRunStatus, error) {
	return waitSwarmRunInterval(ctx, controllerURL, 500*time.Millisecond)
}

func waitSwarmRunInterval(ctx context.Context, controllerURL string, pollInterval time.Duration) (*SwarmRunStatus, error) {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	client := &http.Client{Timeout: 15 * time.Second}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var lastStatus SwarmRunStatus
	lastLoggedState := ""
	lastLoggedRegistered := -1
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(controllerURL, "/v1/status"), nil)
		response, err := client.Do(request)
		if err == nil {
			if response.StatusCode == http.StatusOK {
				decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&lastStatus)
				_ = response.Body.Close()
				if decodeErr != nil {
					return nil, decodeErr
				}
				if lastStatus.State != lastLoggedState || lastStatus.Registered != lastLoggedRegistered {
					log.Printf(
						"Swarm run state=%s peers=%d/%d",
						lastStatus.State,
						lastStatus.Registered,
						lastStatus.Expected,
					)
					lastLoggedState = lastStatus.State
					lastLoggedRegistered = lastStatus.Registered
				}
				switch lastStatus.State {
				case "completed":
					return &lastStatus, nil
				case "failed":
					return &lastStatus, fmt.Errorf("swarm experiment failed: %s", lastStatus.Error)
				}
			} else {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
		}
		select {
		case <-ctx.Done():
			return &lastStatus, fmt.Errorf("wait for swarm experiment state=%s registered=%d/%d: %w",
				lastStatus.State, lastStatus.Registered, lastStatus.Expected, ctx.Err())
		case <-ticker.C:
		}
	}
}

func downloadSwarmArchive(ctx context.Context, controllerURL, resultDir string) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(controllerURL, "/v1/results/archive"), nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 128*1024))
		return fmt.Errorf("download swarm results returned %s: %s", response.Status, string(body))
	}
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return err
	}
	return extractTarGZ(response.Body, resultDir)
}

func extractTarGZ(source io.Reader, destination string) error {
	gzipReader, err := gzip.NewReader(source)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		cleanName := filepath.Clean(filepath.FromSlash(header.Name))
		if cleanName == "." || filepath.IsAbs(cleanName) || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		target := filepath.Join(root, cleanName)
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if targetAbs != root && !strings.HasPrefix(targetAbs, root+string(filepath.Separator)) {
			return fmt.Errorf("archive path escapes result directory: %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetAbs, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(targetAbs, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, tarReader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}
