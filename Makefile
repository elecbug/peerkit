.PHONY: build test validate expand run clean

build:
	go build -o bin/peerkit ./cmd/peerkit
	go build -o bin/peerkit-peer ./cmd/peerkit-peer
	go build -o bin/peerkit-swarm-controller ./cmd/peerkit-swarm-controller

test:
	go test ./...

validate:
	go run ./cmd/peerkit validate examples/edge.yaml
	go run ./cmd/peerkit validate examples/er-domain.yaml
	go run ./cmd/peerkit validate examples/duplicate-aware-domain.yaml
	go run ./cmd/peerkit validate examples/idontwant-domain.yaml
	go run ./cmd/peerkit validate examples/scalable-domain.yaml
	go run ./cmd/peerkit validate examples/swarm-domain.yaml

expand:
	go run ./cmd/peerkit expand -o /tmp/peerkit-resolved-domain.yaml examples/er-domain.yaml

run:
	go run ./cmd/peerkit run examples/er-domain.yaml

clean:
	rm -rf bin .peerkit
