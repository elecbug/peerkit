.PHONY: all build install test check clean

all: build

# Bootstrap the single user-facing CLI. Runtime containers build their internal
# peer and Swarm Controller binaries through deploy/Dockerfile.
build:
	mkdir -p bin
	go build -trimpath -o bin/peerkit ./cmd/peerkit

install:
	go install ./cmd/peerkit

test:
	go test ./...

check:
	@test -z "$$(gofmt -l cmd internal)" || \
		(echo "Go files require formatting:"; gofmt -l cmd internal; exit 1)
	go test ./...
	go vet ./...

clean:
	rm -rf bin .peerkit
