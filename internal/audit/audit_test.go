
package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewCreatesAuditFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "audit.jsonl")

	recorder, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer recorder.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat audit file: %v", err)
	}

	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("file permissions = %04o, want 0600", got)
	}
}

func TestRecordWritesJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	recorder, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer recorder.Close()

	previous := int32(2)
	target := int32(0)

	event := Event{
		Operation:        "disable",
		Namespace:        "portfolio",
		Deployment:       "portfolio",
		PreviousReplicas: &previous,
		TargetReplicas:   &target,
		Result:           "success",
	}

	if err := recorder.Record(event); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}

	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal audit event: %v", err)
	}

	if got.Operation != event.Operation {
		t.Errorf("operation = %q, want %q", got.Operation, event.Operation)
	}

	if got.Namespace != event.Namespace {
		t.Errorf("namespace = %q, want %q", got.Namespace, event.Namespace)
	}

	if got.Deployment != event.Deployment {
		t.Errorf("deployment = %q, want %q", got.Deployment, event.Deployment)
	}

	if got.PreviousReplicas == nil || *got.PreviousReplicas != previous {
		t.Errorf("previous replicas = %v, want %d", got.PreviousReplicas, previous)
	}

	if got.TargetReplicas == nil || *got.TargetReplicas != target {
		t.Errorf("target replicas = %v, want %d", got.TargetReplicas, target)
	}

	if got.Result != event.Result {
		t.Errorf("result = %q, want %q", got.Result, event.Result)
	}

	if got.Timestamp.IsZero() {
		t.Error("expected timestamp to be populated")
	}
}

func TestRecordAppendsEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	recorder, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer recorder.Close()

	for i := 0; i < 2; i++ {
		if err := recorder.Record(Event{
			Operation: "disable",
			Namespace: "portfolio",
			Deployment: "portfolio",
			Result:    "success",
		}); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0

	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("invalid JSONL record: %v", err)
		}
		count++
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan audit file: %v", err)
	}

	if count != 2 {
		t.Errorf("record count = %d, want 2", count)
	}
}

func TestRecordUsesUTC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	recorder, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer recorder.Close()

	location := time.FixedZone("IST", 5*60*60+30*60)

	event := Event{
		Timestamp: time.Date(2026, 11, 6, 12, 0, 0, 0, location),
		Operation: "enable",
		Result:    "success",
	}

	if err := recorder.Record(event); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}

	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal audit event: %v", err)
	}

	if got.Timestamp.Location() != time.UTC {
		t.Errorf("timestamp location = %v, want UTC", got.Timestamp.Location())
	}
}

func TestRecordConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	recorder, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer recorder.Close()

	const workers = 10
	const recordsPerWorker = 5

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < recordsPerWorker; j++ {
				err := recorder.Record(Event{
					Operation: "disable",
					Namespace: "portfolio",
					Deployment: "portfolio",
					Result:    "success",
				})
				if err != nil {
					t.Errorf("Record() error = %v", err)
				}
			}
		}()
	}

	wg.Wait()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0

	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("invalid JSONL record: %v", err)
		}
		count++
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan audit file: %v", err)
	}

	expected := workers * recordsPerWorker

	if count != expected {
		t.Errorf("record count = %d, want %d", count, expected)
	}
}

func TestRecordAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	recorder, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := recorder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	err = recorder.Record(Event{
		Operation: "disable",
		Result:    "success",
	})

	if err == nil {
		t.Fatal("expected error when recording after close")
	}
}

func TestNewRejectsEmptyPath(t *testing.T) {
	_, err := New("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

