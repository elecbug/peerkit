package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	peerkitp2p "github.com/k-p2plab/peerkit/internal/p2p"
)

type controlEndpoints map[string]string

type controlClient struct {
	http        *http.Client
	parallelism int
}

func newControlClient(parallelism int) *controlClient {
	if parallelism <= 0 {
		parallelism = 32
	}
	return &controlClient{
		http: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        parallelism * 2,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     30 * time.Second,
			},
			Timeout: 15 * time.Second,
		},
		parallelism: parallelism,
	}
}

func endpointsFromPorts(ports map[string]int) controlEndpoints {
	values := make(controlEndpoints, len(ports))
	for node, port := range ports {
		values[node] = fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	return values
}

func (c *controlClient) waitReady(ctx context.Context, endpoints controlEndpoints) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	remaining := cloneEndpoints(endpoints)
	for len(remaining) > 0 {
		ready := make(chan string, len(remaining))
		_ = c.forEach(ctx, remaining, func(ctx context.Context, node, baseURL string) error {
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(baseURL, "/healthz"), nil)
			response, err := c.http.Do(request)
			if err != nil {
				return nil
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready <- node
			}
			return nil
		})
		close(ready)
		for node := range ready {
			delete(remaining, node)
		}
		if len(remaining) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("peers not ready: %v: %w", sortedEndpointKeys(remaining), ctx.Err())
		case <-ticker.C:
		}
	}
	return nil
}

func (c *controlClient) connectAll(ctx context.Context, endpoints controlEndpoints) error {
	return c.postAll(ctx, endpoints, "/v1/connect", "connect")
}

func (c *controlClient) prepareAll(ctx context.Context, endpoints controlEndpoints) error {
	return c.postAll(ctx, endpoints, "/v1/prepare", "prepare")
}

func (c *controlClient) finalizeAll(ctx context.Context, endpoints controlEndpoints) error {
	return c.postAll(ctx, endpoints, "/v1/finalize", "finalize")
}

func (c *controlClient) postAll(ctx context.Context, endpoints controlEndpoints, path, operation string) error {
	return c.forEach(ctx, endpoints, func(ctx context.Context, node, baseURL string) error {
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(baseURL, path), nil)
		response, err := c.http.Do(request)
		if err != nil {
			return fmt.Errorf("%s %s: %w", operation, node, err)
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		_ = response.Body.Close()
		if response.StatusCode >= 300 {
			return fmt.Errorf("%s %s returned %s: %s", operation, node, response.Status, string(body))
		}
		return nil
	})
}

func (c *controlClient) waitTopology(ctx context.Context, endpoints controlEndpoints) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var mismatchMu sync.Mutex
		mismatched := make([]string, 0)
		_ = c.forEach(ctx, endpoints, func(ctx context.Context, node, baseURL string) error {
			status, err := c.status(ctx, baseURL)
			if err != nil {
				mismatchMu.Lock()
				mismatched = append(mismatched, node+": "+err.Error())
				mismatchMu.Unlock()
				return nil
			}
			sort.Strings(status.ConnectedNeighbors)
			sort.Strings(status.ExpectedNeighbors)
			if !reflect.DeepEqual(status.ConnectedNeighbors, status.ExpectedNeighbors) {
				mismatchMu.Lock()
				mismatched = append(mismatched, fmt.Sprintf("%s connected=%v expected=%v", node, status.ConnectedNeighbors, status.ExpectedNeighbors))
				mismatchMu.Unlock()
			}
			return nil
		})
		if len(mismatched) == 0 {
			return nil
		}
		sort.Strings(mismatched)
		select {
		case <-ctx.Done():
			return fmt.Errorf("topology did not converge: %v: %w", mismatched, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *controlClient) forEach(
	ctx context.Context,
	endpoints controlEndpoints,
	fn func(context.Context, string, string) error,
) error {
	sem := make(chan struct{}, c.parallelism)
	errCh := make(chan error, len(endpoints))
	var wg sync.WaitGroup
	for node, baseURL := range endpoints {
		node, baseURL := node, baseURL
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
			defer func() { <-sem }()
			if err := fn(ctx, node, baseURL); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	var errs []error
	for err := range errCh {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *controlClient) status(ctx context.Context, baseURL string) (peerkitp2p.StatusResponse, error) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(baseURL, "/v1/status"), nil)
	response, err := c.http.Do(request)
	if err != nil {
		return peerkitp2p.StatusResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		return peerkitp2p.StatusResponse{}, fmt.Errorf("status %s: %s", response.Status, string(body))
	}
	var status peerkitp2p.StatusResponse
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return status, err
	}
	return status, nil
}

func (c *controlClient) inject(ctx context.Context, baseURL string, requestBody peerkitp2p.InjectRequest) error {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(baseURL, "/v1/inject"), bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		return fmt.Errorf("inject returned %s: %s", response.Status, string(body))
	}
	return nil
}

func (c *controlClient) downloadResults(ctx context.Context, endpoints controlEndpoints, resultDir string) error {
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return err
	}
	downloadClient := &http.Client{Transport: c.http.Transport}
	return c.forEach(ctx, endpoints, func(ctx context.Context, node, baseURL string) error {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(baseURL, "/v1/results"), nil)
		response, err := downloadClient.Do(request)
		if err != nil {
			return fmt.Errorf("download results from %s: %w", node, err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
			return fmt.Errorf("download results from %s returned %s: %s", node, response.Status, string(body))
		}
		path := filepath.Join(resultDir, node+".jsonl")
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, response.Body)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func endpoint(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}

func sortedEndpointKeys(values controlEndpoints) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneEndpoints(values controlEndpoints) controlEndpoints {
	result := make(controlEndpoints, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
