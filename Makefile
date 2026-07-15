.PHONY: build test validate expand run clean

build:
	go build -o bin/peerkit ./cmd/peerkit
	go build -o bin/peerkit-peer ./cmd/peerkit-peer

test:
	go test ./...

validate:
	go run ./cmd/peerkit validate examples/edge.yaml
	go run ./cmd/peerkit validate examples/er-domain.yaml

expand:
	go run ./cmd/peerkit expand -o /tmp/peerkit-resolved-domain.yaml examples/er-domain.yaml

run:
	go run ./cmd/peerkit run examples/edge.yaml

clean:
	rm -rf bin .peerkit
