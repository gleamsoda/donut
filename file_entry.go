package donut

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

type FileEntry struct {
	Path      string      `json:"-"`
	Empty     bool        `json:"empty"`
	Mode      fs.FileMode `json:"mode"`
	ModTime   time.Time   `json:"mod_time"`
	sum       []byte
	content   []byte
	IsFetched bool `json:"-"`
}

var _ json.Marshaler = (*FileEntry)(nil)
var _ json.Unmarshaler = (*FileEntry)(nil)

func NewFileEntry(path string) (*FileEntry, error) {
	f, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &FileEntry{
				Path:  path,
				Empty: true,
			}, nil
		} else {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}

	return &FileEntry{
		Path:    path,
		Empty:   false,
		Mode:    f.Mode(),
		ModTime: f.ModTime(),
	}, nil
}

func (e *FileEntry) GetSum() ([]byte, error) {
	if e == nil || e.Empty {
		return nil, nil
	}
	if e.sum == nil && !e.IsFetched {
		if err := e.loadSum(); err != nil {
			return nil, err
		}
	}
	return e.sum, nil
}

func (e *FileEntry) GetContent() ([]byte, error) {
	if e == nil || e.Empty {
		return nil, nil
	}
	if e.content == nil && !e.IsFetched {
		if err := e.loadContent(); err != nil {
			return nil, err
		}
	}
	return e.content, nil
}

func (e *FileEntry) IsDir() bool {
	return e.Mode.IsDir()
}

func (e *FileEntry) IsSymLink() bool {
	return e.Mode&os.ModeSymlink != 0
}

func (f *FileEntry) IsSame(path string) (bool, error) {
	if !f.IsSymLink() {
		return f.Path == path, nil
	}
	l, err := os.Readlink(f.Path)
	if err != nil {
		return false, fmt.Errorf("%s: %w", f.Path, err)
	}
	return l == path, nil
}

// MarshalJSON implements json.Marshaler interface.
func (e *FileEntry) MarshalJSON() ([]byte, error) {
	type Alias FileEntry // エイリアスを作成して、再帰的な呼び出しを避ける

	sum, err := e.GetSum()
	if err != nil {
		return nil, err
	}
	content, err := e.GetContent()
	if err != nil {
		return nil, err
	}
	return json.Marshal(&struct {
		*Alias
		Sum     []byte `json:"sum"`
		Content []byte `json:"content"`
	}{
		Alias:   (*Alias)(e),
		Sum:     sum,
		Content: content,
	})
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (e *FileEntry) UnmarshalJSON(value []byte) error {
	type Alias FileEntry // エイリアスを作成して、再帰的な呼び出しを避ける

	aux := &struct {
		*Alias
		Sum     []byte `json:"sum"`
		Content []byte `json:"content"`
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(value, &aux); err != nil {
		return err
	}

	e.sum = aux.Sum
	e.content = aux.Content
	e.IsFetched = true
	return nil
}

func (e *FileEntry) loadSum() error {
	file, err := os.Open(e.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return err
	}
	e.sum = h.Sum(nil)
	return nil
}

func (e *FileEntry) loadContent() error {
	r, err := os.ReadFile(e.Path)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := h.Write(r); err != nil {
		return err
	}
	e.content = r
	e.sum = h.Sum(nil)
	return nil
}
