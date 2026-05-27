package llm_resolver

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// statusWriter wraps http.ResponseWriter to capture the status code
// while forwarding the common optional interfaces (Flusher, Hijacker)
// that downstream handlers like reverse_proxy rely on for streaming
// and WebSocket upgrades.
type statusWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func newStatusWriter(w http.ResponseWriter) *statusWriter {
	return &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.written {
		s.statusCode = code
		s.written = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	s.written = true
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func (s *statusWriter) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
