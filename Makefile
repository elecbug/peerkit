.PHONY: build test validate expand run clean

build:
	go build -o bin/peerkit ./cmd/peerkit
	go build -o bin/peerkit-peer ./cmd/peerkit-peer

test:
	go test ./...

validate:
	go run ./cmd/peerkit validate examples/ring.yaml
	go run ./cmd/peerkit validate examples/domain.yml

expand:
	go run ./cmd/peerkit expand -o /tmp/peerkit-resolved-domain.yaml examples/domain.yml

run:
	go run ./cmd/peerkit run examples/ring.yaml

clean:
	rm -rf bin .peerkit
