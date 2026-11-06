package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	Timestamp        time.Time `json:"timestamp"`
	Operation        string    `json:"operation"`
	Namespace        string    `json:"namespace"`
	Deployment       string    `json:"deployment"`
	PreviousReplicas *int32    `json:"previous_replicas,omitempty"`
	TargetReplicas   *int32    `json:"target_replicas,omitempty"`
	Result           string    `json:"result"`
	Error            string    `json:"error,omitempty"`
}

type Sink interface {
	Record(event Event) error
}

type Recorder struct {
	mu     sync.Mutex
	file   *os.File
	closed bool
}

func New(path string) (*Recorder, error) {
	if path == "" {
		return nil, errors.New("audit file path cannot be empty")
	}

	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(
		path,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0600,
	)
	if err != nil {
		return nil, err
	}

	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}

	return &Recorder{
		file: file,
	}, nil
}

func (r *Recorder) Record(event Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return errors.New("audit recorder is closed")
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}

	encoder := json.NewEncoder(r.file)

	if err := encoder.Encode(event); err != nil {
		return err
	}

	return r.file.Sync()
}

func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true

	return r.file.Close()
}
