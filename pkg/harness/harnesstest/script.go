package harnesstest

import (
	"bytes"
	"io"
	"strings"
	"sync"
)

// ScriptedIO is a line-oriented, in-memory child-process connection.
type ScriptedIO struct {
	mu      sync.Mutex
	inbound chan []byte
	current *bytes.Reader
	writes  []string
	closed  bool
}

func NewScriptedIO(lines ...string) *ScriptedIO {
	capacity := max(len(lines)+32, 512)
	s := &ScriptedIO{inbound: make(chan []byte, capacity)}
	for _, line := range lines {
		s.Push(line)
	}
	return s
}

func (s *ScriptedIO) Push(line string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	s.inbound <- []byte(line)
	return true
}

func (s *ScriptedIO) Read(p []byte) (int, error) {
	for {
		s.mu.Lock()
		if s.current != nil && s.current.Len() > 0 {
			n, err := s.current.Read(p)
			s.mu.Unlock()
			return n, err
		}
		s.current = nil
		inbound := s.inbound
		s.mu.Unlock()

		line, ok := <-inbound
		if !ok {
			return 0, io.EOF
		}
		s.mu.Lock()
		s.current = bytes.NewReader(line)
		s.mu.Unlock()
	}
}

func (s *ScriptedIO) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(p), "\n"), "\n") {
		if line != "" {
			s.writes = append(s.writes, line)
		}
	}
	return len(p), nil
}

func (s *ScriptedIO) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.inbound)
	return nil
}

func (s *ScriptedIO) Writes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.writes...)
}

func (s *ScriptedIO) WriteCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.writes)
}

func (s *ScriptedIO) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}
