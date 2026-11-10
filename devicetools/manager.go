// Package devicetools implements authenticated, receiver-only native tools.
package devicetools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const ChunkSize int64 = 512 << 10
const MaxFileSize int64 = 16 << 30

type ownerKey struct{}

func WithOwner(r *http.Request, owner string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ownerKey{}, owner))
}
func owner(r *http.Request) string { value, _ := r.Context().Value(ownerKey{}).(string); return value }

type Config struct {
	Shared, State, Version string
	Telemetry              func(context.Context) map[string]any
	Quality                func(owner, token string, bitrate int) (any, error)
}

type Manager struct {
	config  Config
	root    *os.Root
	mux     *http.ServeMux
	filesMu sync.Mutex
	jobsMu  sync.Mutex
	jobs    map[string]*Job
	closed  context.Context
	cancel  context.CancelFunc
}

func New(config Config) (*Manager, error) {
	if !filepath.IsAbs(config.Shared) || !filepath.IsAbs(config.State) {
		return nil, errors.New("absolute tool directories required")
	}
	if err := os.MkdirAll(config.State, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(config.Shared)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{config: config, root: root, mux: http.NewServeMux(), jobs: map[string]*Job{}, closed: ctx, cancel: cancel}
	m.mux.HandleFunc("GET /api/tools/status", m.status)
	m.mux.HandleFunc("GET /api/tools/files", m.list)
	m.mux.HandleFunc("GET /api/tools/files/download", m.download)
	m.mux.HandleFunc("POST /api/tools/files/mkdir", m.mkdir)
	m.mux.HandleFunc("POST /api/tools/files/remove", m.remove)
	m.mux.HandleFunc("POST /api/tools/uploads", m.beginUpload)
	m.mux.HandleFunc("GET /api/tools/uploads/{id}", m.uploadStatus)
	m.mux.HandleFunc("PUT /api/tools/uploads/{id}", m.uploadChunk)
	m.mux.HandleFunc("POST /api/tools/uploads/{id}/finish", m.finishUpload)
	m.mux.HandleFunc("DELETE /api/tools/uploads/{id}", m.cancelUpload)
	m.mux.HandleFunc("POST /api/tools/terminal/jobs", m.terminal)
	m.mux.HandleFunc("GET /api/tools/terminal/jobs/{id}", m.jobStatus)
	m.mux.HandleFunc("POST /api/tools/terminal/jobs/{id}/cancel", m.cancelJob)
	m.mux.HandleFunc("POST /api/tools/apk/install", m.installAPK)
	m.mux.HandleFunc("POST /api/tools/quality", m.quality)
	return m, nil
}

func (m *Manager) Close() { m.cancel(); _ = m.root.Close() }
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if owner(r) == "" {
		respond(w, 401, map[string]any{"error": "unauthorized"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	m.mux.ServeHTTP(w, r)
}
func respond(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, code int, message string) {
	respond(w, code, map[string]any{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(value); err != nil {
		problem(w, 400, "invalid_request")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		problem(w, 400, "invalid_request")
		return false
	}
	return true
}
func id() string {
	var value [20]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}
func atomic(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tools-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func clean(value string) (string, error) {
	if value == "" || value == "." {
		return ".", nil
	}
	if strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") {
		return "", errors.New("invalid_path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || strings.HasPrefix(part, ".halfcloud-") {
			return "", errors.New("invalid_path")
		}
	}
	return filepath.FromSlash(value), nil
}
func (m *Manager) status(w http.ResponseWriter, r *http.Request) {
	value := map[string]any{"version": m.config.Version, "root": os.Geteuid() == 0, "uptime_seconds": time.Since(started).Seconds(), "shared_storage": "/sdcard", "capabilities": []string{"files", "resumable_upload", "range_download", "terminal", "apk_install", "device_status", "live_bitrate"}}
	if m.config.Telemetry != nil {
		value["device"] = m.config.Telemetry(r.Context())
	}
	respond(w, 200, value)
}

var started = time.Now()

func (m *Manager) quality(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token   string `json:"token"`
		Bitrate int    `json:"bitrate"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Bitrate < 500000 || body.Bitrate > 30000000 || len(body.Token) > 100 {
		problem(w, 400, "invalid_bitrate")
		return
	}
	if m.config.Quality == nil {
		problem(w, 501, "quality_unavailable")
		return
	}
	result, err := m.config.Quality(owner(r), body.Token, body.Bitrate)
	if err != nil {
		problem(w, 409, err.Error())
		return
	}
	respond(w, 200, result)
}
