package p2p

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/k-p2plab/peerkit/internal/config"
)

type SwarmRegisterRequest struct {
	Slot       int    `json:"slot"`
	PeerID     string `json:"peer_id"`
	Address    string `json:"address"`
	ControlURL string `json:"control_url"`
}

type SwarmRegisterResponse struct {
	NodeID     string `json:"node_id"`
	Expected   int    `json:"expected"`
	Registered int    `json:"registered"`
}

// BootstrapRuntime generates an ephemeral identity, registers the task with the
// Swarm controller, and waits until the controller can resolve all peer
// identities into a complete runtime configuration.
func BootstrapRuntime(ctx context.Context, controllerURL string, slot int) (*config.RuntimeNodeConfig, error) {
	if slot <= 0 {
		return nil, fmt.Errorf("swarm task slot must be positive")
	}
	controllerURL = strings.TrimRight(controllerURL, "/")
	if _, err := url.ParseRequestURI(controllerURL); err != nil {
		return nil, fmt.Errorf("invalid swarm controller URL: %w", err)
	}

	privateKey, _, err := crypto.GenerateEd25519Key(cryptorand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate swarm peer identity: %w", err)
	}
	privateKeyBytes, err := crypto.MarshalPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshal swarm peer identity: %w", err)
	}
	peerID, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("derive swarm peer id: %w", err)
	}

	localIP, err := discoverSwarmTaskIP(ctx, controllerURL, os.Getenv("PEERKIT_OVERLAY_CIDR"))
	if err != nil {
		return nil, err
	}
	registration := SwarmRegisterRequest{
		Slot:       slot,
		PeerID:     peerID.String(),
		Address:    fmt.Sprintf("/ip4/%s/tcp/4001", localIP.String()),
		ControlURL: fmt.Sprintf("http://%s:8080", localIP.String()),
	}

	client := &http.Client{Timeout: 10 * time.Second}
	if err := registerWithRetry(ctx, client, controllerURL, registration); err != nil {
		return nil, err
	}
	cfg, err := waitRuntimeConfig(ctx, client, controllerURL, slot)
	if err != nil {
		return nil, err
	}
	cfg.PrivateKey = base64.StdEncoding.EncodeToString(privateKeyBytes)
	return cfg, nil
}

func discoverSwarmTaskIP(ctx context.Context, controllerURL, overlayCIDR string) (net.IP, error) {
	overlayCIDR = strings.TrimSpace(overlayCIDR)
	if overlayCIDR == "" {
		return discoverControllerRouteIP(ctx, controllerURL)
	}
	_, network, err := net.ParseCIDR(overlayCIDR)
	if err != nil {
		return nil, fmt.Errorf("parse PEERKIT_OVERLAY_CIDR %q: %w", overlayCIDR, err)
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil {
				continue
			}
			ip = ip.To4()
			if ip != nil && network.Contains(ip) {
				return ip, nil
			}
		}
	}
	return nil, fmt.Errorf("no local IPv4 address belongs to overlay subnet %s", overlayCIDR)
}

func discoverControllerRouteIP(ctx context.Context, controllerURL string) (net.IP, error) {
	parsed, err := url.Parse(controllerURL)
	if err != nil {
		return nil, err
	}
	host := parsed.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	var lastErr error
	for {
		connection, err := dialer.DialContext(ctx, "tcp", host)
		if err == nil {
			address, ok := connection.LocalAddr().(*net.TCPAddr)
			_ = connection.Close()
			if !ok || address.IP == nil || address.IP.To4() == nil {
				return nil, fmt.Errorf("controller route did not provide an IPv4 address")
			}
			return address.IP.To4(), nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("discover controller route: %w", errorsJoin(lastErr, ctx.Err()))
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func registerWithRetry(ctx context.Context, client *http.Client, controllerURL string, registration SwarmRegisterRequest) error {
	payload, err := json.Marshal(registration)
	if err != nil {
		return err
	}
	var lastErr error
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, controllerURL+"/v1/peers/register", bytes.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
			_ = response.Body.Close()
			if response.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("register returned %s: %s", response.Status, string(body))
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("register swarm peer: %w", errorsJoin(lastErr, ctx.Err()))
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func waitRuntimeConfig(ctx context.Context, client *http.Client, controllerURL string, slot int) (*config.RuntimeNodeConfig, error) {
	path := controllerURL + "/v1/peers/config?slot=" + strconv.Itoa(slot)
	var lastErr error
	for {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		response, err := client.Do(request)
		if err == nil {
			if response.StatusCode == http.StatusOK {
				var cfg config.RuntimeNodeConfig
				decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4*1024*1024)).Decode(&cfg)
				_ = response.Body.Close()
				if decodeErr != nil {
					return nil, fmt.Errorf("decode swarm runtime config: %w", decodeErr)
				}
				return &cfg, nil
			}
			body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
			_ = response.Body.Close()
			if response.StatusCode != http.StatusTooEarly && response.StatusCode != http.StatusConflict {
				lastErr = fmt.Errorf("runtime config returned %s: %s", response.Status, string(body))
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for swarm runtime config: %w", errorsJoin(lastErr, ctx.Err()))
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func errorsJoin(values ...error) error {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value != nil {
			parts = append(parts, value.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(parts, ": "))
}
