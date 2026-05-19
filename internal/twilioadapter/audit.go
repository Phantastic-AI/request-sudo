package twilioadapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditLog writes adapter audit rows to a sandbox JSONL file. This is
// NOT the broker's canon — only request-sudod writes canon. The audit
// log is for the adapter's own decisions (SMS sent, replies received,
// routing fallbacks) and is read-only from the broker's perspective.
//
// File format: one JSON object per line, with a `type` discriminator
// and a `wall_ts` RFC3339 UTC timestamp.
type AuditLog struct {
	path string
	mu   sync.Mutex
}

// NewAuditLog opens (or creates) the audit log file. Parent dir must
// already exist.
func NewAuditLog(path string) (*AuditLog, error) {
	if path == "" {
		return nil, fmt.Errorf("audit log path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log %s: %w", abs, err)
	}
	_ = file.Close()
	return &AuditLog{path: abs}, nil
}

// Write appends an audit row (any JSON-serializable struct). Row must
// embed a `type` field for downstream parsing.
func (a *AuditLog) Write(row map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := row["wall_ts"]; !ok {
		row["wall_ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(row)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(a.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(payload, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

// Path returns the absolute path of the audit log file.
func (a *AuditLog) Path() string { return a.path }
