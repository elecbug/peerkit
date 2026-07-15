package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"sync"
	"time"

	peerkitp2p "github.com/k-p2plab/peerkit/internal/p2p"
)

type controlClient struct {
	http        *http.Client
	parallelism int
}

func newControlClient(parallelism int) *controlClient {
	if parallelism <= 0 {
		parallelism = 32
	}
	return &controlClient{
		http:        &http.Client{Timeout: 5 * time.Second},
		parallelism: parallelism,
	}
}

func (c *controlClient) waitReady(ctx context.Context, ports map[string]int) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	remaining := make(map[string]int, len(ports))
	for node, port := range ports {
		remaining[node] = port
	}
	for len(remaining) > 0 {
		ready := make(chan string, len(remaining))
		_ = c.forEach(ctx, remaining, func(ctx context.Context, node string, port int) error {
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(port, "/healthz"), nil)
			response, err := c.http.Do(request)
			if err != nil {
				return nil
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
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
			return fmt.Errorf("peers not ready: %v: %w", sortedKeys(remaining), ctx.Err())
		case <-ticker.C:
		}
	}
	return nil
}

func (c *controlClient) connectAll(ctx context.Context, ports map[string]int) error {
	return c.forEach(ctx, ports, func(ctx context.Context, node string, port int) error {
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(port, "/v1/connect"), nil)
		response, err := c.http.Do(request)
		if err != nil {
			return fmt.Errorf("connect %s: %w", node, err)
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		response.Body.Close()
		if response.StatusCode >= 300 {
			return fmt.Errorf("connect %s returned %s: %s", node, response.Status, string(body))
		}
		return nil
	})
}

func (c *controlClient) prepareAll(ctx context.Context, ports map[string]int) error {
	return c.forEach(ctx, ports, func(ctx context.Context, node string, port int) error {
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(port, "/v1/prepare"), nil)
		response, err := c.http.Do(request)
		if err != nil {
			return fmt.Errorf("prepare %s: %w", node, err)
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		response.Body.Close()
		if response.StatusCode >= 300 {
			return fmt.Errorf("prepare %s returned %s: %s", node, response.Status, string(body))
		}
		return nil
	})
}

func (c *controlClient) waitTopology(ctx context.Context, ports map[string]int) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var mismatchMu sync.Mutex
		mismatched := make([]string, 0)
		_ = c.forEach(ctx, ports, func(ctx context.Context, node string, port int) error {
			status, err := c.status(ctx, port)
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
	ports map[string]int,
	fn func(context.Context, string, int) error,
) error {
	sem := make(chan struct{}, c.parallelism)
	errCh := make(chan error, len(ports))
	var wg sync.WaitGroup
	for node, port := range ports {
		node, port := node, port
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
			if err := fn(ctx, node, port); err != nil {
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

func (c *controlClient) status(ctx context.Context, port int) (peerkitp2p.StatusResponse, error) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(port, "/v1/status"), nil)
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

func (c *controlClient) inject(ctx context.Context, port int, requestBody peerkitp2p.InjectRequest) error {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(port, "/v1/inject"), bytes.NewReader(payload))
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

func endpoint(port int, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
