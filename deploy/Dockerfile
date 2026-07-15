FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/peerkit-peer ./cmd/peerkit-peer

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /out/peerkit-peer /usr/local/bin/peerkit-peer
ENTRYPOINT ["/usr/local/bin/peerkit-peer"]
