package metrics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Writer struct {
	mu      sync.Mutex
	file    *os.File
	buffer  *bufio.Writer
	encoder *json.Encoder
}

func NewWriter(path string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create event directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create event file: %w", err)
	}
	buffer := bufio.NewWriterSize(file, 64*1024)
	return &Writer{file: file, buffer: buffer, encoder: json.NewEncoder(buffer)}, nil
}

func (w *Writer) Write(event Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.encoder.Encode(event); err != nil {
		return err
	}
	return w.buffer.Flush()
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	flushErr := w.buffer.Flush()
	closeErr := w.file.Close()
	w.file = nil
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}
