.PHONY: build test validate run clean

build:
	go build -o bin/peerkit ./cmd/peerkit
	go build -o bin/peerkit-peer ./cmd/peerkit-peer

test:
	go test ./...

validate:
	go run ./cmd/peerkit validate examples/ring.yaml

run:
	go run ./cmd/peerkit run examples/ring.yaml

clean:
	rm -rf bin .peerkit
