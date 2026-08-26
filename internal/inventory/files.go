package inventory

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	defaultFileLimit = 256
	defaultByteLimit = 8 * 1024 * 1024
)

var (
	ErrFileOutsideTrustedRoot = errors.New("inventory: file outside trusted root")
	ErrFileBudgetExceeded     = errors.New("inventory: file budget exceeded")
)

type FileBudget struct {
	MaxFiles int
	MaxBytes int64
}

type FileReader struct {
	root  string
	quota *SharedFileBudget
}

type SharedFileBudget struct {
	maxFiles int
	maxBytes int64

	mu        sync.Mutex
	filesRead int
	bytesRead int64
}

type FileRecord struct {
	Name    string
	Content []byte
}

func NewFileReader(trustedRoot string) (*FileReader, error) {
	return NewFileReaderWithBudget(trustedRoot, FileBudget{
		MaxFiles: defaultFileLimit,
		MaxBytes: defaultByteLimit,
	})
}

func NewFileReaderWithBudget(trustedRoot string, budget FileBudget) (*FileReader, error) {
	quota, err := NewSharedFileBudget(budget)
	if err != nil {
		return nil, err
	}
	return NewFileReaderWithSharedBudget(trustedRoot, quota)
}

func NewSharedFileBudget(budget FileBudget) (*SharedFileBudget, error) {
	if budget.MaxFiles <= 0 || budget.MaxBytes <= 0 {
		return nil, errors.New("inventory: file budget must be positive")
	}
	return &SharedFileBudget{maxFiles: budget.MaxFiles, maxBytes: budget.MaxBytes}, nil
}

func NewFileReaderWithSharedBudget(trustedRoot string, quota *SharedFileBudget) (*FileReader, error) {
	if strings.TrimSpace(trustedRoot) == "" || !filepath.IsAbs(trustedRoot) {
		return nil, errors.New("inventory: trusted file root must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(trustedRoot))
	if err != nil {
		return nil, errors.New("inventory: trusted file root is unavailable")
	}
	if quota == nil {
		return nil, errors.New("inventory: shared file budget is required")
	}
	return &FileReader{root: resolved, quota: quota}, nil
}

func (r *FileReader) ReadWorkspace(relativePath string) ([]byte, error) {
	if strings.TrimSpace(relativePath) == "" || filepath.IsAbs(relativePath) {
		return nil, ErrFileOutsideTrustedRoot
	}
	cleaned := filepath.Clean(relativePath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return nil, ErrFileOutsideTrustedRoot
	}
	candidate := filepath.Join(r.root, cleaned)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, errors.New("inventory: file is unavailable")
	}
	contained, err := filepath.Rel(r.root, resolved)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) || filepath.IsAbs(contained) {
		return nil, ErrFileOutsideTrustedRoot
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("inventory: file is unavailable")
	}

	r.quota.mu.Lock()
	defer r.quota.mu.Unlock()
	if r.quota.filesRead >= r.quota.maxFiles || info.Size() > r.quota.maxBytes-r.quota.bytesRead {
		return nil, ErrFileBudgetExceeded
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, errors.New("inventory: file is unavailable")
	}
	defer file.Close()
	remaining := r.quota.maxBytes - r.quota.bytesRead
	payload, err := io.ReadAll(io.LimitReader(file, remaining+1))
	if err != nil {
		return nil, errors.New("inventory: file is unavailable")
	}
	if int64(len(payload)) > remaining {
		return nil, ErrFileBudgetExceeded
	}
	r.quota.filesRead++
	r.quota.bytesRead += int64(len(payload))
	return payload, nil
}

func (r *FileReader) ReadMatches(relativePattern string) ([]FileRecord, error) {
	if strings.TrimSpace(relativePattern) == "" || filepath.IsAbs(relativePattern) {
		return nil, ErrFileOutsideTrustedRoot
	}
	cleaned := filepath.Clean(relativePattern)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return nil, ErrFileOutsideTrustedRoot
	}
	matches, err := filepath.Glob(filepath.Join(r.root, cleaned))
	if err != nil {
		return nil, errors.New("inventory: invalid file pattern")
	}
	records := make([]FileRecord, 0, len(matches))
	for _, match := range matches {
		relative, err := filepath.Rel(r.root, match)
		if err != nil {
			return nil, ErrFileOutsideTrustedRoot
		}
		content, err := r.ReadWorkspace(relative)
		if err != nil {
			return nil, err
		}
		records = append(records, FileRecord{Name: filepath.ToSlash(relative), Content: content})
	}
	return records, nil
}
