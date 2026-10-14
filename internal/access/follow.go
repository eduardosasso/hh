package access

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

// Poll is how often Follow checks the file for new lines and rotation.
const Poll = 200 * time.Millisecond

// Options shape what Follow emits.
type Options struct {
	// Last is how many existing entries to emit before following. Zero emits none.
	Last int
	// Keep filters entries, both existing and new. Nil keeps all.
	Keep func(Entry) bool
	// Warn receives lines that do not parse (wrapping ErrLine) and file errors.
	Warn func(error)
}

type follower struct {
	path    string
	opts    Options
	file    *os.File
	reader  *bufio.Reader
	pending []byte
	offset  int64
}

// Follow emits the last entries of path, then every new one until ctx is done,
// like tail -f. It reopens the file when it is rotated or truncated, and waits
// for it when it does not exist yet.
func Follow(ctx context.Context, path string, o Options) (<-chan Entry, error) {
	f := &follower{path: path, opts: o}
	if err := f.open(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	history := f.drain()
	if o.Last < len(history) {
		history = history[len(history)-o.Last:]
	}
	out := make(chan Entry)
	go f.run(ctx, history, out)

	return out, nil
}

func (f *follower) run(ctx context.Context, history []Entry, out chan<- Entry) {
	defer close(out)
	defer f.close()
	tick := time.NewTicker(Poll)
	defer tick.Stop()
	for batch := history; ; batch = f.poll() {
		for _, e := range batch {
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
	}
}

// poll reads what is new, switching to a new file after rotation.
func (f *follower) poll() []Entry {
	st, err := os.Stat(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		// Rotated away and not recreated yet: finish the old file.
		return f.drain()
	}
	if err != nil {
		f.warn(fmt.Errorf("stat %s: %w", f.path, err))

		return nil
	}
	if f.file == nil {
		return f.reopen(nil)
	}
	cur, err := f.file.Stat()
	if err != nil {
		f.warn(fmt.Errorf("stat %s: %w", f.path, err))

		return nil
	}
	if !os.SameFile(cur, st) {
		return f.reopen(f.drain())
	}
	if st.Size() < f.offset {
		f.rewind()
	}

	return f.drain()
}

// reopen switches to the file now at path and reads it from the start.
func (f *follower) reopen(done []Entry) []Entry {
	f.close()
	if err := f.open(); err != nil {
		f.warn(err)

		return done
	}

	return append(done, f.drain()...)
}

func (f *follower) open() error {
	file, err := os.Open(f.path)
	if err != nil {
		return fmt.Errorf("open request log: %w", err)
	}
	f.file, f.reader, f.pending, f.offset = file, bufio.NewReader(file), nil, 0

	return nil
}

// rewind starts over after the file was truncated in place.
func (f *follower) rewind() {
	if _, err := f.file.Seek(0, io.SeekStart); err != nil {
		f.warn(fmt.Errorf("rewind %s: %w", f.path, err))

		return
	}
	f.reader.Reset(f.file)
	f.pending, f.offset = nil, 0
}

func (f *follower) close() {
	if f.file == nil {
		return
	}
	if err := f.file.Close(); err != nil {
		f.warn(fmt.Errorf("close %s: %w", f.path, err))
	}
	f.file = nil
}

// drain reads every complete line available. A partial last line waits for the next poll.
func (f *follower) drain() []Entry {
	if f.file == nil {
		return nil
	}
	var out []Entry
	for {
		chunk, err := f.reader.ReadBytes('\n')
		f.pending = append(f.pending, chunk...)
		f.offset += int64(len(chunk))
		if err != nil {
			if !errors.Is(err, io.EOF) {
				f.warn(fmt.Errorf("read %s: %w", f.path, err))
			}

			return out
		}
		line := bytes.TrimSpace(f.pending)
		f.pending = nil
		if e, ok := f.parse(line); ok {
			out = append(out, e)
		}
	}
}

func (f *follower) parse(line []byte) (Entry, bool) {
	if len(line) == 0 {
		return Entry{}, false
	}
	e, err := Parse(line)
	if err != nil {
		f.warn(err)

		return Entry{}, false
	}

	return e, f.opts.Keep == nil || f.opts.Keep(e)
}

func (f *follower) warn(err error) {
	if f.opts.Warn != nil {
		f.opts.Warn(err)
	}
}
