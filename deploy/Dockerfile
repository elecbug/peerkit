FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/peerkit-peer ./cmd/peerkit-peer
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/peerkit-swarm-controller ./cmd/peerkit-swarm-controller

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /out/peerkit-peer /usr/local/bin/peerkit-peer
COPY --from=build /out/peerkit-swarm-controller /usr/local/bin/peerkit-swarm-controller
ENTRYPOINT ["/usr/local/bin/peerkit-peer"]
