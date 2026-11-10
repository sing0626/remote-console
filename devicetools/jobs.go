package devicetools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const outputLimit = 1 << 20

type Job struct {
	mu              sync.Mutex
	ID, Owner, Kind string
	Output          []byte
	Base            int64
	Done            bool
	Code            int
	Created         time.Time
	Cancel          context.CancelFunc
}

func (j *Job) Write(data []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	size := len(data)
	j.Output = append(j.Output, data...)
	if len(j.Output) > outputLimit {
		drop := len(j.Output) - outputLimit
		j.Base += int64(drop)
		j.Output = j.Output[drop:]
	}
	return size, nil
}
func (m *Manager) startJob(w http.ResponseWriter, r *http.Request, kind string, seconds int, build func(context.Context) (*exec.Cmd, io.Closer, error)) {
	m.jobsMu.Lock()
	active := 0
	for key, job := range m.jobs {
		job.mu.Lock()
		if job.Done && time.Since(job.Created) > 15*time.Minute {
			delete(m.jobs, key)
		} else if !job.Done {
			active++
		}
		job.mu.Unlock()
	}
	if active >= 8 || len(m.jobs) >= 64 {
		m.jobsMu.Unlock()
		problem(w, 429, "too_many_jobs")
		return
	}
	ctx, cancel := context.WithTimeout(m.closed, time.Duration(seconds)*time.Second)
	command, closer, err := build(ctx)
	if err != nil {
		cancel()
		m.jobsMu.Unlock()
		problem(w, 400, "command_unavailable")
		return
	}
	job := &Job{ID: id(), Owner: owner(r), Kind: kind, Created: time.Now(), Cancel: cancel, Code: -1}
	command.Stdout = job
	command.Stderr = job
	configureProcess(command)
	if err = command.Start(); err != nil {
		cancel()
		if closer != nil {
			closer.Close()
		}
		m.jobsMu.Unlock()
		problem(w, 500, "command_start_failed")
		return
	}
	m.jobs[job.ID] = job
	m.jobsMu.Unlock()
	go func() {
		err := command.Wait()
		if closer != nil {
			closer.Close()
		}
		job.mu.Lock()
		job.Done = true
		job.Code = 0
		if err != nil {
			job.Code = command.ProcessState.ExitCode()
			if ctx.Err() != nil {
				job.Output = append(job.Output, []byte("\n[已停止：取消或執行逾時]\n")...)
			}
		}
		job.mu.Unlock()
		cancel()
	}()
	respond(w, 202, map[string]any{"id": job.ID, "kind": kind})
}
func (m *Manager) terminal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command   string `json:"command"`
		Directory string `json:"directory"`
		Timeout   int    `json:"timeout_seconds"`
	}
	if !decode(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Command) == "" || len(body.Command) > 32768 {
		problem(w, 400, "invalid_command")
		return
	}
	if body.Timeout == 0 {
		body.Timeout = 30
	}
	if body.Timeout < 1 || body.Timeout > 120 {
		problem(w, 400, "invalid_timeout")
		return
	}
	if body.Directory != "" {
		info, err := os.Stat(body.Directory)
		if err != nil || !info.IsDir() || !filepath.IsAbs(body.Directory) {
			problem(w, 400, "invalid_working_directory")
			return
		}
	}
	m.startJob(w, r, "terminal", body.Timeout, func(ctx context.Context) (*exec.Cmd, io.Closer, error) {
		shell := "/bin/sh"
		if runtime.GOOS == "android" {
			shell = "/system/bin/sh"
		}
		cmd := exec.CommandContext(ctx, shell, "-c", body.Command)
		cmd.Dir = body.Directory
		return cmd, nil, nil
	})
}
func (m *Manager) job(r *http.Request) *Job {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()
	job := m.jobs[r.PathValue("id")]
	if job != nil && job.Owner == owner(r) {
		return job
	}
	return nil
}
func (m *Manager) jobStatus(w http.ResponseWriter, r *http.Request) {
	job := m.job(r)
	if job == nil {
		problem(w, 404, "job_unavailable")
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if r.URL.Query().Get("offset") == "" {
		offset = 0
		err = nil
	}
	if err != nil || offset < 0 {
		problem(w, 400, "invalid_offset")
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	start := max(offset, job.Base)
	end := min(start+65536, job.Base+int64(len(job.Output)))
	if start > end {
		problem(w, 400, "invalid_offset")
		return
	}
	// Keep polling offsets on UTF-8 boundaries so split Chinese/emoji output survives pagination.
	if end < job.Base+int64(len(job.Output)) {
		for end > start && !utf8.RuneStart(job.Output[end-job.Base]) {
			end--
		}
	}
	respond(w, 200, map[string]any{"id": job.ID, "kind": job.Kind, "output": strings.ToValidUTF8(string(job.Output[start-job.Base:end-job.Base]), "�"), "offset": end, "truncated": offset < job.Base, "done": job.Done && end == job.Base+int64(len(job.Output)), "exit_code": job.Code})
}
func (m *Manager) cancelJob(w http.ResponseWriter, r *http.Request) {
	job := m.job(r)
	if job == nil {
		problem(w, 404, "job_unavailable")
		return
	}
	job.Cancel()
	respond(w, 200, map[string]bool{"cancelled": true})
}
func (m *Manager) installAPK(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "android" {
		problem(w, 501, "android_only")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &body) {
		return
	}
	path, err := clean(body.Path)
	if err != nil || !strings.EqualFold(filepath.Ext(path), ".apk") {
		problem(w, 400, "invalid_apk")
		return
	}
	m.startJob(w, r, "apk_install", 120, func(ctx context.Context) (*exec.Cmd, io.Closer, error) {
		f, err := m.root.Open(path)
		if err != nil {
			return nil, nil, err
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			f.Close()
			return nil, nil, errors.New("invalid apk")
		}
		cmd := exec.CommandContext(ctx, "/system/bin/pm", "install", "-r", "--user", "0", "-S", strconv.FormatInt(info.Size(), 10))
		// A pipe avoids passing a FUSE-labelled /sdcard fd to system_server over Binder.
		cmd.Stdin = io.NewSectionReader(f, 0, info.Size())
		return cmd, f, nil
	})
}
