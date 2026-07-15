package p2p

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const maxFrameSize = 4 * 1024 * 1024

type WireMessage struct {
	ID           string `json:"id"`
	Origin       string `json:"origin"`
	Sequence     uint64 `json:"sequence"`
	CreatedAtNS  int64  `json:"created_at_ns"`
	Hop          uint32 `json:"hop"`
	PayloadBytes int    `json:"payload_bytes"`
}

func writeFrame(writer *bufio.Writer, message WireMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal wire message: %w", err)
	}
	if len(payload) > maxFrameSize {
		return fmt.Errorf("frame too large: %d bytes", len(payload))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}

func readFrame(reader *bufio.Reader) (WireMessage, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return WireMessage{}, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > maxFrameSize {
		return WireMessage{}, fmt.Errorf("invalid frame length %d", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return WireMessage{}, err
	}
	var message WireMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return WireMessage{}, fmt.Errorf("decode wire message: %w", err)
	}
	return message, nil
}
