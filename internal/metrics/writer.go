package metrics

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/k-p2plab/peerkit/internal/config"
)

// Writer serializes event production through a bounded in-memory queue and
// flushes JSONL output in batches. This avoids a filesystem write on every
// simulation event while preserving event order within a peer.
type Writer struct {
	mu     sync.RWMutex
	closed bool
	events chan Event
	done   chan struct{}
	err    error

	file          *os.File
	buffer        *bufio.Writer
	encoder       *json.Encoder
	flushInterval time.Duration
}

func NewWriter(path string, cfg config.MetricsConfig) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create event directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create event file: %w", err)
	}
	if cfg.BufferBytes < 4096 {
		cfg.BufferBytes = 256 * 1024
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = 512
	}
	if cfg.FlushIntervalMS <= 0 {
		cfg.FlushIntervalMS = 200
	}
	buffer := bufio.NewWriterSize(file, cfg.BufferBytes)
	w := &Writer{
		events:        make(chan Event, cfg.QueueCapacity),
		done:          make(chan struct{}),
		file:          file,
		buffer:        buffer,
		encoder:       json.NewEncoder(buffer),
		flushInterval: time.Duration(cfg.FlushIntervalMS) * time.Millisecond,
	}
	go w.run()
	return w, nil
}

func (w *Writer) Write(event Event) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return errors.New("metrics writer is closed")
	}
	w.events <- event
	return nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		<-w.done
		return w.err
	}
	w.closed = true
	close(w.events)
	w.mu.Unlock()
	<-w.done
	return w.err
}

func (w *Writer) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-w.events:
			if !ok {
				w.finish()
				return
			}
			if w.err == nil {
				if err := w.encoder.Encode(event); err != nil {
					w.err = err
				}
			}
		case <-ticker.C:
			if w.err == nil {
				if err := w.buffer.Flush(); err != nil {
					w.err = err
				}
			}
		}
	}
}

func (w *Writer) finish() {
	if w.err == nil {
		w.err = w.buffer.Flush()
	}
	closeErr := w.file.Close()
	if w.err == nil {
		w.err = closeErr
	}
}
