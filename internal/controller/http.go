package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"time"

	peerkitp2p "github.com/k-p2plab/peerkit/internal/p2p"
)

type controlClient struct {
	http *http.Client
}

func newControlClient() *controlClient {
	return &controlClient{http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *controlClient) waitReady(ctx context.Context, ports map[string]int) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	remaining := make(map[string]int, len(ports))
	for node, port := range ports {
		remaining[node] = port
	}
	for len(remaining) > 0 {
		for node, port := range remaining {
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(port, "/healthz"), nil)
			response, err := c.http.Do(request)
			if err == nil {
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					delete(remaining, node)
				}
			}
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
	for node, port := range ports {
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
	}
	return nil
}

func (c *controlClient) prepareAll(ctx context.Context, ports map[string]int) error {
	for node, port := range ports {
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
	}
	return nil
}

func (c *controlClient) waitTopology(ctx context.Context, ports map[string]int) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		mismatched := make([]string, 0)
		for node, port := range ports {
			status, err := c.status(ctx, port)
			if err != nil {
				mismatched = append(mismatched, node+": "+err.Error())
				continue
			}
			sort.Strings(status.ConnectedNeighbors)
			sort.Strings(status.ExpectedNeighbors)
			if !reflect.DeepEqual(status.ConnectedNeighbors, status.ExpectedNeighbors) {
				mismatched = append(mismatched, fmt.Sprintf("%s connected=%v expected=%v", node, status.ConnectedNeighbors, status.ExpectedNeighbors))
			}
		}
		if len(mismatched) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("topology did not converge: %v: %w", mismatched, ctx.Err())
		case <-ticker.C:
		}
	}
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
