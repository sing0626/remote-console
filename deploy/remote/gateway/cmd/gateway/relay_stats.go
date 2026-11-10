package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type relayNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}
type relayReport struct {
	ID      string     `json:"id"`
	At      int64      `json:"at"`
	Day     relayUsage `json:"day"`
	Month   relayUsage `json:"month"`
	Total   relayUsage `json:"total"`
	QuotaGB float64    `json:"quota_gb"`
	Count   string     `json:"count"`
	Active  bool       `json:"active"`
}
type relayUsage struct {
	RX uint64 `json:"rx"`
	TX uint64 `json:"tx"`
}
type relayMonitor struct {
	sync.Mutex
	nodes   map[string]relayNode
	reports map[string]relayReport
	path    string
}

func loadRelayMonitor(dir string) (*relayMonitor, error) {
	m := &relayMonitor{nodes: map[string]relayNode{}, reports: map[string]relayReport{}, path: filepath.Join(dir, "relay-reports.json")}
	b, e := os.ReadFile(filepath.Join(dir, "relay-nodes.json"))
	if os.IsNotExist(e) {
		return m, nil
	}
	if e != nil {
		return nil, e
	}
	var nodes []relayNode
	if e = json.Unmarshal(b, &nodes); e != nil {
		return nil, e
	}
	if len(nodes) > 4 {
		return nil, fmt.Errorf("too many relay nodes")
	}
	for _, n := range nodes {
		key, e := hex.DecodeString(n.Key)
		if n.ID == "" || len(n.ID) > 64 || len(n.Name) > 80 || e != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid relay node")
		}
		m.nodes[n.ID] = n
	}
	if b, e = os.ReadFile(m.path); e == nil {
		if e = json.Unmarshal(b, &m.reports); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	return m, nil
}
func (m *relayMonitor) receive(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 8193))
	if e != nil || len(b) > 8192 {
		http.Error(w, "invalid report", 400)
		return
	}
	var report relayReport
	if json.Unmarshal(b, &report) != nil || report.QuotaGB < 0 || report.QuotaGB > 1e9 || (report.Count != "tx" && report.Count != "total") {
		http.Error(w, "invalid report", 400)
		return
	}
	n, ok := m.nodes[report.ID]
	ts := r.Header.Get("X-HalfCloud-Time")
	at, e := strconv.ParseInt(ts, 10, 64)
	if !ok || e != nil || at != report.At || time.Since(time.Unix(at, 0)).Abs() > 5*time.Minute {
		http.Error(w, "unauthorized", 401)
		return
	}
	key, _ := hex.DecodeString(n.Key)
	sum := sha256.Sum256(b)
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "%s\n%x", ts, sum)
	sig, e := hex.DecodeString(r.Header.Get("X-HalfCloud-Signature"))
	if e != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		http.Error(w, "unauthorized", 401)
		return
	}
	m.Lock()
	defer m.Unlock()
	if report.At <= m.reports[report.ID].At {
		http.Error(w, "stale report", 409)
		return
	}
	next := map[string]relayReport{}
	for k, v := range m.reports {
		next[k] = v
	}
	next[report.ID] = report
	payload, _ := json.Marshal(next)
	f, e := os.CreateTemp(filepath.Dir(m.path), ".relay-report-")
	if e != nil {
		http.Error(w, "storage", 500)
		return
	}
	name := f.Name()
	defer os.Remove(name)
	f.Chmod(0600)
	_, e = f.Write(payload)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(name, m.path)
	}
	if e != nil {
		http.Error(w, "storage", 500)
		return
	}
	m.reports = next
	w.WriteHeader(204)
}
func (m *relayMonitor) status(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	items := []map[string]any{}
	for id, n := range m.nodes {
		v, has := m.reports[id]
		item := map[string]any{"id": id, "name": n.Name, "available": has && time.Since(time.Unix(v.At, 0)) < 3*time.Minute}
		if has {
			item["report"] = v
			used := v.Month.TX
			if v.Count == "total" {
				used += v.Month.RX
			}
			if v.QuotaGB > 0 {
				fraction := float64(used) / (v.QuotaGB * 1e9)
				item["quota_fraction"] = fraction
				if fraction >= .95 {
					item["warning"] = "流量已達參考額度 95%"
				} else if fraction >= .8 {
					item["warning"] = "流量已達參考額度 80%"
				}
			}
		}
		items = append(items, item)
	}
	authJSON(w, 200, map[string]any{"relays": items, "scope": "interface", "timezone": "Asia/Hong_Kong"})
}
