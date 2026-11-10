package devicetools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
	Modified  int64  `json:"modified"`
}

func (m *Manager) list(w http.ResponseWriter, r *http.Request) {
	path, err := clean(r.URL.Query().Get("path"))
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	f, err := m.root.Open(path)
	if err != nil {
		problem(w, 404, "directory_unavailable")
		return
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		problem(w, 400, "not_a_directory")
		return
	}
	items := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".halfcloud-") {
			continue
		}
		info, e := m.root.Stat(filepath.Join(path, entry.Name()))
		if e != nil {
			continue
		}
		items = append(items, Entry{entry.Name(), filepath.ToSlash(filepath.Join(path, entry.Name())), info.IsDir(), info.Size(), info.ModTime().UnixMilli()})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Directory != items[j].Directory {
			return items[i].Directory
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	start, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if start < 0 || start > len(items) {
		problem(w, 400, "invalid_offset")
		return
	}
	end := min(start+200, len(items))
	next := -1
	if end < len(items) {
		next = end
	}
	respond(w, 200, map[string]any{"path": filepath.ToSlash(path), "entries": items[start:end], "next_offset": next})
}
func (m *Manager) download(w http.ResponseWriter, r *http.Request) {
	path, err := clean(r.URL.Query().Get("path"))
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	f, err := m.root.Open(path)
	if err != nil {
		problem(w, 404, "file_unavailable")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		problem(w, 400, "not_a_file")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name()}))
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size()))
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
func (m *Manager) mkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &body) {
		return
	}
	path, err := clean(body.Path)
	if err != nil || path == "." {
		problem(w, 400, "invalid_path")
		return
	}
	if err = m.root.MkdirAll(path, 0755); err != nil {
		problem(w, 409, "directory_creation_failed")
		return
	}
	respond(w, 200, map[string]bool{"created": true})
}
func (m *Manager) remove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &body) {
		return
	}
	path, err := clean(body.Path)
	if err != nil || path == "." {
		problem(w, 400, "invalid_path")
		return
	}
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	if err = m.root.Remove(path); err != nil {
		problem(w, 409, "remove_failed_or_directory_not_empty")
		return
	}
	respond(w, 200, map[string]bool{"removed": true})
}

type Upload struct {
	ID        string `json:"id"`
	Owner     string `json:"-"`
	OwnerHash string `json:"owner_hash,omitempty"`
	Path      string `json:"path"`
	Stage     string `json:"stage,omitempty"`
	Size      int64  `json:"size"`
	Offset    int64  `json:"offset"`
	SHA256    string `json:"sha256"`
	Overwrite bool   `json:"overwrite"`
	Complete  bool   `json:"complete"`
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var idPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func ownerHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (m *Manager) uploadFile(id string) string {
	return filepath.Join(m.config.State, "upload-"+id+".json")
}
func (m *Manager) saveUpload(u *Upload) error { return atomic(m.uploadFile(u.ID), u) }
func (m *Manager) loadUpload(id, who string) (*Upload, error) {
	if !idPattern.MatchString(id) {
		return nil, errors.New("invalid_upload")
	}
	data, err := os.ReadFile(m.uploadFile(id))
	if err != nil {
		return nil, err
	}
	var u Upload
	if json.Unmarshal(data, &u) != nil || u.OwnerHash != ownerHash(who) {
		return nil, errors.New("upload_unavailable")
	}
	if !u.Complete {
		info, err := m.root.Stat(u.Stage)
		if err != nil {
			return nil, err
		}
		u.Offset = info.Size()
		if u.Offset > u.Size {
			return nil, errors.New("invalid_upload_length")
		}
	}
	return &u, nil
}
func uploadResponse(w http.ResponseWriter, u *Upload) {
	respond(w, 200, map[string]any{"id": u.ID, "path": u.Path, "size": u.Size, "offset": u.Offset, "complete": u.Complete, "chunk_size": ChunkSize, "sha256": u.SHA256})
}
func (m *Manager) beginUpload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path      string `json:"path"`
		Size      int64  `json:"size"`
		SHA256    string `json:"sha256"`
		Overwrite bool   `json:"overwrite"`
	}
	if !decode(w, r, &body) {
		return
	}
	path, err := clean(body.Path)
	if err != nil || path == "." || body.Size < 0 || body.Size > MaxFileSize || !digestPattern.MatchString(body.SHA256) {
		problem(w, 400, "invalid_upload")
		return
	}
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	manifests, _ := filepath.Glob(filepath.Join(m.config.State, "upload-*.json"))
	for _, manifest := range manifests {
		u, e := m.loadUpload(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(manifest), "upload-"), ".json"), owner(r))
		if e == nil && u.Path == path && u.Size == body.Size && u.SHA256 == body.SHA256 && u.Overwrite == body.Overwrite {
			if !u.Complete {
				uploadResponse(w, u)
				return
			}
			if f, e := m.root.Open(path); e == nil {
				sum := sha256.New()
				_, e = io.Copy(sum, f)
				f.Close()
				if e == nil && hex.EncodeToString(sum.Sum(nil)) == u.SHA256 {
					uploadResponse(w, u)
					return
				}
			}
		}
	}
	if len(manifests) >= 512 {
		// Completed manifests are a retry cache; discard metadata only, retaining the uploaded files.
		remaining := len(manifests)
		for _, path := range manifests {
			data, e := os.ReadFile(path)
			var saved Upload
			if e == nil && json.Unmarshal(data, &saved) == nil && saved.Complete {
				if os.Remove(path) == nil {
					remaining--
				}
			}
		}
		if remaining >= 512 {
			problem(w, 429, "too_many_uploads")
			return
		}
	}
	if !body.Overwrite {
		if _, e := m.root.Lstat(path); e == nil {
			problem(w, 409, "file_exists")
			return
		} else if !errors.Is(e, fs.ErrNotExist) {
			problem(w, 400, "invalid_destination")
			return
		}
	}
	uid := id()
	u := &Upload{ID: uid, OwnerHash: ownerHash(owner(r)), Path: path, Size: body.Size, SHA256: body.SHA256, Overwrite: body.Overwrite, Stage: filepath.Join(filepath.Dir(path), ".halfcloud-"+uid+".part")}
	f, err := m.root.OpenFile(u.Stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		problem(w, 409, "destination_unavailable")
		return
	}
	f.Close()
	if err = m.saveUpload(u); err != nil {
		m.root.Remove(u.Stage)
		problem(w, 500, "upload_state_failed")
		return
	}
	uploadResponse(w, u)
}
func (m *Manager) uploadStatus(w http.ResponseWriter, r *http.Request) {
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	u, err := m.loadUpload(r.PathValue("id"), owner(r))
	if err != nil {
		problem(w, 404, "upload_unavailable")
		return
	}
	uploadResponse(w, u)
}
func (m *Manager) uploadChunk(w http.ResponseWriter, r *http.Request) {
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	u, err := m.loadUpload(r.PathValue("id"), owner(r))
	if err != nil || u.Complete {
		problem(w, 404, "upload_unavailable")
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset != u.Offset {
		problem(w, 409, "offset_mismatch")
		return
	}
	if r.ContentLength < 0 || r.ContentLength > ChunkSize || r.ContentLength > u.Size-u.Offset {
		problem(w, 413, "chunk_too_large")
		return
	}
	f, err := m.root.OpenFile(u.Stage, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		problem(w, 500, "upload_write_failed")
		return
	}
	n, copyErr := io.CopyN(f, r.Body, r.ContentLength)
	syncErr := f.Sync()
	closeErr := f.Close()
	u.Offset += n
	if err = m.saveUpload(u); err != nil || syncErr != nil || closeErr != nil {
		problem(w, 500, "upload_save_failed")
		return
	}
	if copyErr != nil {
		problem(w, 400, "partial_chunk_saved")
		return
	}
	uploadResponse(w, u)
}
func (m *Manager) finishUpload(w http.ResponseWriter, r *http.Request) {
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	u, err := m.loadUpload(r.PathValue("id"), owner(r))
	if err != nil {
		problem(w, 404, "upload_unavailable")
		return
	}
	if u.Complete {
		uploadResponse(w, u)
		return
	}
	if u.Offset != u.Size {
		problem(w, 409, "upload_incomplete")
		return
	}
	f, err := m.root.Open(u.Stage)
	if err != nil {
		problem(w, 500, "upload_unavailable")
		return
	}
	sum := sha256.New()
	_, err = io.Copy(sum, f)
	f.Close()
	if err != nil || hex.EncodeToString(sum.Sum(nil)) != u.SHA256 {
		problem(w, 409, "sha256_mismatch")
		return
	}
	if !u.Overwrite {
		if _, e := m.root.Lstat(u.Path); e == nil {
			problem(w, 409, "file_exists")
			return
		}
	}
	if err = m.root.Rename(u.Stage, u.Path); err != nil {
		problem(w, 409, "upload_commit_failed")
		return
	}
	u.Complete = true
	u.Stage = ""
	if err = m.saveUpload(u); err != nil {
		problem(w, 500, "upload_state_failed")
		return
	}
	uploadResponse(w, u)
}
func (m *Manager) cancelUpload(w http.ResponseWriter, r *http.Request) {
	m.filesMu.Lock()
	defer m.filesMu.Unlock()
	u, err := m.loadUpload(r.PathValue("id"), owner(r))
	if err != nil {
		problem(w, 404, "upload_unavailable")
		return
	}
	if !u.Complete {
		_ = m.root.Remove(u.Stage)
	}
	_ = os.Remove(m.uploadFile(u.ID))
	respond(w, 200, map[string]bool{"cancelled": true})
}
