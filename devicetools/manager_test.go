package devicetools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func call(m *Manager, who, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	if who != "" {
		r = WithOwner(r, who)
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}
func object(t *testing.T, w *httptest.ResponseRecorder, code int) map[string]any {
	t.Helper()
	if w.Code != code {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var v map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func setup(t *testing.T) (*Manager, Config) {
	t.Helper()
	c := Config{Shared: t.TempDir(), State: t.TempDir(), Version: "test"}
	m, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	return m, c
}
func TestResumableUnicodeTransferAndIsolation(t *testing.T) {
	m, c := setup(t)
	data := "檔案\n" + strings.Repeat("abcd", 32768)
	sum := sha256.Sum256([]byte(data))
	path := "測試 😀.txt"
	body, _ := json.Marshal(map[string]any{"path": path, "size": len(data), "sha256": hex.EncodeToString(sum[:])})
	object(t, call(m, "", "GET", "/api/tools/status", ""), 401)
	start := object(t, call(m, "a", "POST", "/api/tools/uploads", string(body)), 200)
	id := start["id"].(string)
	object(t, call(m, "b", "GET", "/api/tools/uploads/"+id, ""), 404)
	object(t, call(m, "a", "PUT", "/api/tools/uploads/"+id+"?offset=1", "bad"), 409)
	object(t, call(m, "a", "PUT", "/api/tools/uploads/"+id+"?offset=0", data[:17000]), 200)
	m.Close()
	var e error
	m, e = New(c)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	again := object(t, call(m, "a", "POST", "/api/tools/uploads", string(body)), 200)
	if again["id"] != id || again["offset"].(float64) != 17000 {
		t.Fatal(again)
	}
	object(t, call(m, "a", "PUT", "/api/tools/uploads/"+id+"?offset=17000", data[17000:]), 200)
	object(t, call(m, "a", "POST", "/api/tools/uploads/"+id+"/finish", ""), 200)
	raw, e := os.ReadFile(filepath.Join(c.Shared, path))
	if e != nil || string(raw) != data {
		t.Fatalf("content: %v", e)
	}
	uri := "/api/tools/files/download?path=" + url.QueryEscape(path)
	full := call(m, "a", "GET", uri, "")
	if full.Code != 200 {
		t.Fatal(full.Code)
	}
	part := call(m, "a", "GET", uri, "", "Range", "bytes=17000-", "If-Range", full.Header().Get("ETag"))
	if part.Code != 206 || part.Body.String() != data[17000:] {
		t.Fatal("range", part.Code)
	}
	changed := call(m, "a", "GET", uri, "", "Range", "bytes=17000-", "If-Range", `"stale"`)
	if changed.Code != 200 || changed.Body.String() != data {
		t.Fatal("stale range")
	}
	object(t, call(m, "a", "DELETE", "/api/tools/uploads/"+id, ""), 200)
	if _, e = os.Stat(filepath.Join(c.Shared, path)); e != nil {
		t.Fatal("cancel removed committed file")
	}
}
func TestPathsAndChecksum(t *testing.T) {
	m, c := setup(t)
	for _, path := range []string{"../secret", "/etc/passwd", "a/../../secret", ".halfcloud-test", "a\\b"} {
		b, _ := json.Marshal(map[string]string{"path": path})
		object(t, call(m, "a", "POST", "/api/tools/files/mkdir", string(b)), 400)
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600)
	if os.Symlink(outside, filepath.Join(c.Shared, "escape")) == nil {
		w := call(m, "a", "GET", "/api/tools/files/download?path=escape/secret", "")
		if w.Code == 200 {
			t.Fatal("symlink escaped root")
		}
	}
	body := `{"path":"bad.txt","size":3,"sha256":"` + strings.Repeat("0", 64) + `"}`
	id := object(t, call(m, "a", "POST", "/api/tools/uploads", body), 200)["id"].(string)
	object(t, call(m, "a", "PUT", "/api/tools/uploads/"+id+"?offset=0", "bad"), 200)
	object(t, call(m, "a", "POST", "/api/tools/uploads/"+id+"/finish", ""), 409)
	if _, e := os.Stat(filepath.Join(c.Shared, "bad.txt")); !os.IsNotExist(e) {
		t.Fatal("bad hash committed")
	}
}
func TestShellOutputExitAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix receiver")
	}
	m, _ := setup(t)
	id := object(t, call(m, "a", "POST", "/api/tools/terminal/jobs", `{"command":"printf hello; printf error >&2; exit 7"}`), 202)["id"].(string)
	object(t, call(m, "b", "GET", "/api/tools/terminal/jobs/"+id, ""), 404)
	for i := 0; i < 100; i++ {
		v := object(t, call(m, "a", "GET", "/api/tools/terminal/jobs/"+id, ""), 200)
		if v["done"] == true {
			if v["exit_code"].(float64) != 7 || !strings.Contains(v["output"].(string), "hello") {
				t.Fatal(v)
			}
			break
		}
		if i == 99 {
			t.Fatal("shell timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	id = object(t, call(m, "a", "POST", "/api/tools/terminal/jobs", `{"command":"sleep 90 & wait","timeout_seconds":120}`), 202)["id"].(string)
	object(t, call(m, "b", "POST", "/api/tools/terminal/jobs/"+id+"/cancel", ""), 404)
	object(t, call(m, "a", "POST", "/api/tools/terminal/jobs/"+id+"/cancel", ""), 200)
	limit := time.Now().Add(4 * time.Second)
	for {
		v := object(t, call(m, "a", "GET", "/api/tools/terminal/jobs/"+id, ""), 200)
		if v["done"] == true {
			break
		}
		if time.Now().After(limit) {
			t.Fatal("cancel left pipes open")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestPartialChunkSaved(t *testing.T) {
	m, _ := setup(t)
	sum := sha256.Sum256([]byte("12345"))
	b, _ := json.Marshal(map[string]any{"path": "partial", "size": 5, "sha256": hex.EncodeToString(sum[:])})
	id := object(t, call(m, "a", "POST", "/api/tools/uploads", string(b)), 200)["id"].(string)
	r := httptest.NewRequest("PUT", "/api/tools/uploads/"+id+"?offset=0", io.NopCloser(strings.NewReader("12")))
	r.ContentLength = 5
	w := httptest.NewRecorder()
	m.ServeHTTP(w, WithOwner(r, "a"))
	object(t, w, 400)
	v := object(t, call(m, "a", "GET", "/api/tools/uploads/"+id, ""), 200)
	if v["offset"].(float64) != 2 {
		t.Fatal(v)
	}
	object(t, call(m, "a", "PUT", "/api/tools/uploads/"+id+"?offset=2", "345"), 200)
	object(t, call(m, "a", "POST", "/api/tools/uploads/"+id+"/finish", ""), 200)
}

func TestUnicodeJobOutputAcrossPollBoundaries(t *testing.T) {
	m, _ := setup(t)
	text := strings.Repeat("中😀", 18000)
	j := &Job{ID: "unicode", Owner: "a", Done: true, Code: 0}
	j.Write([]byte(text))
	m.jobs[j.ID] = j
	output := ""
	offset := int64(0)
	for i := 0; i < 10; i++ {
		v := object(t, call(m, "a", "GET", "/api/tools/terminal/jobs/unicode?offset="+fmt.Sprint(offset), ""), 200)
		output += v["output"].(string)
		offset = int64(v["offset"].(float64))
		if v["done"] == true {
			break
		}
	}
	if output != text {
		t.Fatal("UTF-8 output was damaged at page boundary")
	}
}
