package p2p

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	maxFrameSize        = 4 * 1024 * 1024
	frameTypeData       = "data"
	frameTypeIDontWant  = "idontwant"
	minimumControlBytes = 32
)

type WireMessage struct {
	ID           string `json:"id"`
	Origin       string `json:"origin"`
	Sequence     uint64 `json:"sequence"`
	CreatedAtNS  int64  `json:"created_at_ns"`
	Hop          uint32 `json:"hop"`
	PayloadBytes int    `json:"payload_bytes"`
}

type WireFrame struct {
	Type         string `json:"type"`
	MessageID    string `json:"message_id"`
	Origin       string `json:"origin,omitempty"`
	Sequence     uint64 `json:"sequence,omitempty"`
	CreatedAtNS  int64  `json:"created_at_ns,omitempty"`
	Hop          uint32 `json:"hop,omitempty"`
	PayloadBytes int    `json:"payload_bytes,omitempty"`
}

func newDataFrame(message WireMessage) WireFrame {
	return WireFrame{
		Type: frameTypeData, MessageID: message.ID, Origin: message.Origin,
		Sequence: message.Sequence, CreatedAtNS: message.CreatedAtNS,
		Hop: message.Hop, PayloadBytes: message.PayloadBytes,
	}
}

func newIDontWantFrame(message WireMessage) WireFrame {
	return WireFrame{
		Type: frameTypeIDontWant, MessageID: message.ID,
		Origin: message.Origin, Sequence: message.Sequence,
	}
}

func (f WireFrame) message() WireMessage {
	return WireMessage{
		ID: f.MessageID, Origin: f.Origin, Sequence: f.Sequence,
		CreatedAtNS: f.CreatedAtNS, Hop: f.Hop,
		PayloadBytes: f.PayloadBytes,
	}
}

func (f WireFrame) simulatedBytes() int {
	if f.Type == frameTypeData {
		return f.PayloadBytes
	}
	payload, err := json.Marshal(f)
	if err != nil || len(payload)+4 < minimumControlBytes {
		return minimumControlBytes
	}
	return len(payload) + 4
}

func writeFrame(writer *bufio.Writer, frame WireFrame) error {
	payload, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal wire frame: %w", err)
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

func readFrame(reader *bufio.Reader) (WireFrame, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return WireFrame{}, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > maxFrameSize {
		return WireFrame{}, fmt.Errorf("invalid frame length %d", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return WireFrame{}, err
	}
	var frame WireFrame
	if err := json.Unmarshal(payload, &frame); err != nil {
		return WireFrame{}, fmt.Errorf("decode wire frame: %w", err)
	}
	switch frame.Type {
	case frameTypeData, frameTypeIDontWant:
	default:
		return WireFrame{}, fmt.Errorf("unsupported frame type %q", frame.Type)
	}
	if frame.MessageID == "" {
		return WireFrame{}, fmt.Errorf("wire frame has empty message id")
	}
	return frame, nil
}
