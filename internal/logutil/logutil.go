package logutil

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	MaxFileSize  = 1 << 20 // 1 MB
	MaxBackups   = 3
)

// RotatingWriter writes logs to a file, rotating at MaxFileSize keeping MaxBackups old files.
type RotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxSize  int64
	backups  int
	file     *os.File
	curSize  int64
	alsoEcho bool
}

// NewRotatingWriter opens or creates a rotating log file at path.
func NewRotatingWriter(path string, alsoEcho bool) (*RotatingWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	w := &RotatingWriter{
		path:     path,
		maxSize:  MaxFileSize,
		backups:  MaxBackups,
		alsoEcho: alsoEcho,
	}
	if err := w.openExistingOrNew(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotatingWriter) openExistingOrNew() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.curSize = info.Size()
	return nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.openExistingOrNew(); err != nil {
			return 0, err
		}
	}
	if w.curSize+int64(len(p)) > w.maxSize {
		if err := w.rotateLocked(); err != nil {
			// Best effort: continue writing if rotation fails
			_ = err
		}
	}
	n, err := w.file.Write(p)
	w.curSize += int64(n)
	if w.alsoEcho {
		_, _ = os.Stderr.Write(p)
	}
	return n, err
}

func (w *RotatingWriter) rotateLocked() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	// Shift backups: .2 -> .3, .1 -> .2, current -> .1
	oldest := fmt.Sprintf("%s.%d", w.path, w.backups)
	_ = os.Remove(oldest)
	for i := w.backups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", w.path, i)
		dst := fmt.Sprintf("%s.%d", w.path, i+1)
		_ = os.Rename(src, dst)
	}
	_ = os.Rename(w.path, fmt.Sprintf("%s.1", w.path))
	return w.openExistingOrNew()
}

// Close closes the underlying log file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// SetupLogger configures a standard logger writing to the rotating log file.
func SetupLogger(path string, prefix string, alsoEcho bool) (*log.Logger, *RotatingWriter, error) {
	rw, err := NewRotatingWriter(path, alsoEcho)
	if err != nil {
		return log.New(os.Stderr, prefix, log.LstdFlags), nil, err
	}
	var out io.Writer = rw
	return log.New(out, prefix, log.LstdFlags), rw, nil
}
