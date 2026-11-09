package trust

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrExpired = errors.New("request_expired")
var ErrPending = errors.New("authorization_pending")
var ErrSlow = errors.New("slow_down")
var ErrDenied = errors.New("access_denied")
var ErrLimit = errors.New("too_many_requests")

type Controller struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Created  int64  `json:"created_at"`
	LastUsed int64  `json:"last_used_at"`
}
type record struct {
	Controller
	Hash string `json:"credential_hash"`
}
type Pending struct {
	ID       string `json:"request_id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Code     string `json:"approval_code"`
	Created  int64  `json:"created_at"`
	Expires  int64  `json:"expires_at"`
}
type pendingRecord struct {
	Pending
	SecretHash, Kind, State, Credential string
	Controller                          Controller
	NextPoll                            int64
}
type Ticket struct {
	Pending
	Secret   string `json:"request_secret"`
	Interval int    `json:"interval"`
}
type PollResult struct {
	State, Kind, Credential string
	Controller              Controller
}
type Store struct {
	mu       sync.Mutex
	file     string
	records  map[string]record
	pending  map[string]*pendingRecord
	requests []int64
	now      func() time.Time
}

func Open(file string) (*Store, error) {
	s := &Store{file: file, records: map[string]record{}, pending: map[string]*pendingRecord{}, now: time.Now}
	if file == "" {
		return s, nil
	}
	raw, e := os.ReadFile(file)
	if os.IsNotExist(e) {
		return s, s.saveLocked()
	}
	if e != nil {
		return nil, e
	}
	var state struct {
		Version     int      `json:"version"`
		Controllers []record `json:"controllers"`
	}
	if json.Unmarshal(raw, &state) != nil || state.Version != 1 || len(state.Controllers) > 128 {
		return nil, errors.New("unrecognized controller trust store was preserved")
	}
	for _, r := range state.Controllers {
		hash, e := hex.DecodeString(r.Hash)
		if !validID(r.ID) || !validName(r.Name) || len(r.Platform) > 32 || e != nil || len(hash) != 32 || s.records[r.ID].ID != "" {
			return nil, errors.New("invalid controller record was preserved")
		}
		s.records[r.ID] = r
	}
	return s, nil
}
func validID(id string) bool {
	if len(id) != 22 {
		return false
	}
	for _, c := range id {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func validName(name string) bool {
	return strings.TrimSpace(name) != "" && utf8.RuneCountInString(name) <= 80
}
func credentialHash(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}
func (s *Store) saveLocked() error {
	if s.file == "" {
		return nil
	}
	state := struct {
		Version     int      `json:"version"`
		Controllers []record `json:"controllers"`
	}{Version: 1, Controllers: []record{}}
	for _, r := range s.records {
		state.Controllers = append(state.Controllers, r)
	}
	sort.Slice(state.Controllers, func(i, j int) bool { return state.Controllers[i].ID < state.Controllers[j].ID })
	raw, e := json.MarshalIndent(state, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(s.file), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(s.file), ".trust-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, s.file)
}
func (s *Store) cleanupLocked() {
	now := s.now().Unix()
	for id, r := range s.pending {
		if r.Expires <= now {
			delete(s.pending, id)
		}
	}
	keep := s.requests[:0]
	for _, at := range s.requests {
		if at > now-60 {
			keep = append(keep, at)
		}
	}
	s.requests = keep
}
func (s *Store) Request(name, platform, kind string) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	var t Ticket
	if !validName(name) || len(platform) > 32 || (kind != "browser" && kind != "native") {
		return t, errors.New("invalid_controller")
	}
	if len(s.pending) >= 32 || len(s.requests) >= 12 || len(s.records) >= 128 {
		return t, ErrLimit
	}
	id, e := Random(24)
	if e != nil {
		return t, e
	}
	secret, e := Random(32)
	if e != nil {
		return t, e
	}
	alphabet := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, e = rand.Read(b); e != nil {
		return t, e
	}
	for i := range b {
		b[i] = alphabet[int(b[i])&31]
	}
	now := s.now().Unix()
	p := Pending{id, strings.TrimSpace(name), platform, string(b[:4]) + "-" + string(b[4:]), now, now + 300}
	s.pending[id] = &pendingRecord{Pending: p, SecretHash: credentialHash(secret), Kind: kind, State: "pending"}
	s.requests = append(s.requests, now)
	return Ticket{p, secret, 2}, nil
}
func (s *Store) Poll(id, secret string) (PollResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	var result PollResult
	p := s.pending[id]
	if p == nil || subtle.ConstantTimeCompare([]byte(p.SecretHash), []byte(credentialHash(secret))) != 1 {
		return result, ErrExpired
	}
	if p.State == "denied" {
		return result, ErrDenied
	}
	if p.State == "approved" {
		if _, exists := s.records[p.Controller.ID]; !exists {
			return result, ErrDenied
		}
		return PollResult{"approved", p.Kind, p.Credential, p.Controller}, nil
	}
	if p.NextPoll > s.now().Unix() {
		return result, ErrSlow
	}
	p.NextPoll = s.now().Unix() + 2
	return result, ErrPending
}
func (s *Store) Cancel(id, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pending[id]
	if p == nil || subtle.ConstantTimeCompare([]byte(p.SecretHash), []byte(credentialHash(secret))) != 1 {
		return ErrExpired
	}
	if p.State != "pending" {
		return ErrDenied
	}
	delete(s.pending, id)
	return nil
}
func (s *Store) Approve(id string, allow bool) (Controller, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	var empty Controller
	p := s.pending[id]
	if p == nil || p.State != "pending" {
		return empty, ErrExpired
	}
	if !allow {
		p.State = "denied"
		return empty, nil
	}
	if len(s.records) >= 128 {
		return empty, ErrLimit
	}
	ident, e := Random(16)
	if e != nil {
		return empty, e
	}
	secret, e := Random(32)
	if e != nil {
		return empty, e
	}
	c := Controller{ident, p.Name, p.Platform, s.now().Unix(), s.now().Unix()}
	s.records[ident] = record{c, credentialHash(secret)}
	if e = s.saveLocked(); e != nil {
		delete(s.records, ident)
		return empty, e
	}
	p.State = "approved"
	p.Controller = c
	p.Credential = ident + "." + secret
	return c, nil
}
func (s *Store) Authenticate(credential string) (Controller, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty Controller
	parts := strings.Split(credential, ".")
	if len(parts) != 2 || !validID(parts[0]) || len(parts[1]) != 43 {
		return empty, false
	}
	r, ok := s.records[parts[0]]
	if !ok || subtle.ConstantTimeCompare([]byte(r.Hash), []byte(credentialHash(parts[1]))) != 1 {
		return empty, false
	}
	return r.Controller, true
}
func (s *Store) Active(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.records[id]
	return ok
}
func (s *Store) List() ([]Controller, []Pending) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked()
	controllers := []Controller{}
	pending := []Pending{}
	for _, r := range s.records {
		controllers = append(controllers, r.Controller)
	}
	for _, p := range s.pending {
		if p.State == "pending" {
			pending = append(pending, p.Pending)
		}
	}
	sort.Slice(controllers, func(i, j int) bool { return controllers[i].Created < controllers[j].Created })
	sort.Slice(pending, func(i, j int) bool { return pending[i].Created < pending[j].Created })
	return controllers, pending
}
func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.records[id]
	if !ok {
		return errors.New("controller_not_found")
	}
	delete(s.records, id)
	if e := s.saveLocked(); e != nil {
		s.records[id] = old
		return e
	}
	return nil
}
