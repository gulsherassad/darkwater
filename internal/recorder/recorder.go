package recorder

import (
	"bufio"
	"os"
	"path/filepath"
	"time"
)

// Recorder appends raw messages to hourly JSONL files.
type Recorder struct {
	dir  string
	hour string
	f    *os.File
	w    *bufio.Writer
}

func New(dir string) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Recorder{dir: dir}, nil
}

// Write saves one message as one line, switching files when the hour changes.
func (r *Recorder) Write(msg []byte, now time.Time) error {
	hour := now.UTC().Format("2006-01-02T15")
	if hour != r.hour {
		if err := r.rotate(hour); err != nil {
			return err
		}
	}
	if _, err := r.w.Write(msg); err != nil {
		return err
	}
	return r.w.WriteByte('\n')
}

func (r *Recorder) rotate(hour string) error {
	r.Close()
	path := filepath.Join(r.dir, hour+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	r.f, r.w, r.hour = f, bufio.NewWriter(f), hour
	return nil
}

// Flush pushes buffered lines to disk.
func (r *Recorder) Flush() error {
	if r.w == nil {
		return nil
	}
	return r.w.Flush()
}

func (r *Recorder) Close() error {
	r.Flush()
	if r.f == nil {
		return nil
	}
	return r.f.Close()
}
