package metrics

type Event struct {
	TimestampNS     int64   `json:"timestamp_ns"`
	RunID           string  `json:"run_id"`
	Experiment      string  `json:"experiment"`
	Type            string  `json:"type"`
	Node            string  `json:"node"`
	PeerID          string  `json:"peer_id,omitempty"`
	MessageID       string  `json:"message_id,omitempty"`
	Origin          string  `json:"origin,omitempty"`
	From            string  `json:"from,omitempty"`
	To              string  `json:"to,omitempty"`
	Sequence        uint64  `json:"sequence,omitempty"`
	Hop             uint32  `json:"hop,omitempty"`
	PayloadBytes    int     `json:"payload_bytes,omitempty"`
	Duplicate       bool    `json:"duplicate,omitempty"`
	QueueWaitNS     int64   `json:"queue_wait_ns,omitempty"`
	ProcessingNS    int64   `json:"processing_ns,omitempty"`
	EdgeDelayNS     int64   `json:"edge_delay_ns,omitempty"`
	SerializationNS int64   `json:"serialization_ns,omitempty"`
	Connected       int     `json:"connected,omitempty"`
	Expected        int     `json:"expected,omitempty"`
	Reason          string  `json:"reason,omitempty"`
	LossRate        float64 `json:"loss_rate,omitempty"`
}
