package events

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Log struct {
	path     string
	mu       sync.Mutex
	lastHash string
}

func NewLog(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	_ = file.Close()

	log := &Log{path: path}
	events, err := log.Replay()
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		log.lastHash = events[len(events)-1].Hash
	}
	return log, nil
}

func (l *Log) Append(event Event) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if event.EventID == "" {
		event.EventID = randomID("evt")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	event.PrevHash = l.lastHash

	hash, err := hashEvent(event)
	if err != nil {
		return Event{}, err
	}
	event.Hash = hash

	file, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Event{}, err
	}
	defer file.Close()

	line, err := json.Marshal(event)
	if err != nil {
		return Event{}, err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		return Event{}, err
	}
	if err := file.Sync(); err != nil {
		return Event{}, err
	}

	l.lastHash = event.Hash
	return event, nil
}

func (l *Log) Replay() ([]Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.replayUnlocked()
}

func (l *Log) replayUnlocked() ([]Event, error) {
	file, err := os.Open(l.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var out []Event
	var prevHash string
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		if event.PrevHash != prevHash {
			return nil, fmt.Errorf("hash chain mismatch for %s: prev=%q want=%q", event.EventID, event.PrevHash, prevHash)
		}
		wantHash, err := hashEvent(Event{EventID: event.EventID, PrevHash: event.PrevHash, RequestID: event.RequestID, Timestamp: event.Timestamp, Actor: event.Actor, Type: event.Type, Details: event.Details})
		if err != nil {
			return nil, fmt.Errorf("rehash event: %w", err)
		}
		if event.Hash != wantHash {
			return nil, fmt.Errorf("event hash mismatch for %s", event.EventID)
		}
		out = append(out, event)
		prevHash = event.Hash
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type hashEnvelope struct {
	EventID   string          `json:"event_id"`
	PrevHash  string          `json:"prev_hash,omitempty"`
	RequestID string          `json:"request_id"`
	Timestamp time.Time       `json:"timestamp"`
	Actor     any             `json:"actor"`
	Type      string          `json:"type"`
	Details   json.RawMessage `json:"details,omitempty"`
}

func hashEvent(event Event) (string, error) {
	payload, err := json.Marshal(hashEnvelope{
		EventID:   event.EventID,
		PrevHash:  event.PrevHash,
		RequestID: event.RequestID,
		Timestamp: event.Timestamp.UTC(),
		Actor:     event.Actor,
		Type:      event.Type,
		Details:   event.Details,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func randomID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(buf))
}
