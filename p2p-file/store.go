package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var errFileNotFound = errors.New("file not found")

// fileStore persists uploaded and fetched files on disk and keeps metadata in-memory.
type fileStore struct {
	dir      string
	metaPath string

	mu    sync.RWMutex
	files map[string]FileMeta
}

// FileMeta describes an on-disk file entry.
type FileMeta struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	StoredName string    `json:"storedName"`
	Size       int64     `json:"size"`
	AddedAt    time.Time `json:"addedAt"`
	Source     string    `json:"source,omitempty"`
}

// FileInfo is the public view returned to the UI clients.
type FileInfo struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	AddedAt time.Time `json:"addedAt"`
	Source  string    `json:"source,omitempty"`
}

func newFileStore(dir string) (*fileStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("empty storage directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fs := &fileStore{
		dir:      dir,
		metaPath: filepath.Join(dir, "files.json"),
		files:    make(map[string]FileMeta),
	}
	if err := fs.load(); err != nil {
		return nil, err
	}
	return fs, nil
}

func (s *fileStore) load() error {
	data, err := os.ReadFile(s.metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var entries []FileMeta
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	for _, entry := range entries {
		// Skip missing files silently so the UI only shows valid entries.
		if _, err := os.Stat(filepath.Join(s.dir, entry.StoredName)); err == nil {
			s.files[entry.ID] = entry
		}
	}
	return nil
}

func (s *fileStore) Save(name string, src io.Reader, source string) (FileMeta, error) {
	if name == "" {
		name = "file"
	}
	id := randomID()
	storedName := fmt.Sprintf("%s_%s", id, sanitizeFilename(name))
	if storedName == fmt.Sprintf("%s_", id) {
		storedName = id
	}
	tmpPath := filepath.Join(s.dir, storedName+".tmp")
	dstPath := filepath.Join(s.dir, storedName)

	f, err := os.Create(tmpPath)
	if err != nil {
		return FileMeta{}, err
	}
	defer func() { _ = f.Close() }()
	size, err := io.Copy(f, src)
	if err != nil {
		_ = os.Remove(tmpPath)
		return FileMeta{}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return FileMeta{}, err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return FileMeta{}, err
	}

	meta := FileMeta{
		ID:         id,
		Name:       name,
		StoredName: storedName,
		Size:       size,
		AddedAt:    time.Now().UTC(),
		Source:     source,
	}

	s.mu.Lock()
	s.files[id] = meta
	err = s.persistLocked()
	s.mu.Unlock()
	return meta, err
}

func (s *fileStore) List() []FileInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FileInfo, 0, len(s.files))
	for _, meta := range s.files {
		out = append(out, meta.Public())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AddedAt.After(out[j].AddedAt)
	})
	return out
}

func (s *fileStore) Open(id string) (*os.File, FileMeta, error) {
	s.mu.RLock()
	meta, ok := s.files[id]
	s.mu.RUnlock()
	if !ok {
		return nil, FileMeta{}, errFileNotFound
	}
	file, err := os.Open(filepath.Join(s.dir, meta.StoredName))
	if err != nil {
		return nil, FileMeta{}, err
	}
	return file, meta, nil
}

func (s *fileStore) Get(id string) (FileMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta, ok := s.files[id]
	if !ok {
		return FileMeta{}, errFileNotFound
	}
	return meta, nil
}

func (s *fileStore) persistLocked() error {
	entries := make([]FileMeta, 0, len(s.files))
	for _, meta := range s.files {
		entries = append(entries, meta)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].AddedAt.Before(entries[j].AddedAt)
	})
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath, data, 0o644)
}

func (m FileMeta) Public() FileInfo {
	return FileInfo{
		ID:      m.ID,
		Name:    m.Name,
		Size:    m.Size,
		AddedAt: m.AddedAt,
		Source:  m.Source,
	}
}

func randomID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// fallback to current time for the extremely unlikely case rand fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func sanitizeFilename(name string) string {
	const maxLen = 60
	var (
		b     strings.Builder
		count int
	)
	for _, r := range name {
		if count >= maxLen {
			break
		}
		if r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
			count++
			continue
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			count++
			continue
		}
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
			count++
			continue
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
			count++
			continue
		}
		if r == ' ' {
			b.WriteRune('-')
			count++
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "file"
	}
	return out
}
