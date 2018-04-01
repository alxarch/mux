package mux

import (
	"time"
)

type sessionOptions struct {
	MaxFrameSize      int
	MaxWindowSize     int32
	KeepAliveEnabled  bool
	KeepAliveInterval time.Duration
	KeepAliveTimeout  time.Duration
	KeepAliveRetries  int
	AcceptBacklog     int
	RecvBufferSize    int
	SendBufferSize    int
	WriteTimeout      time.Duration
}

const (
	initialAcceptBacklog = 16
	maxFrameSize         = 1<<16 - headerSize
	minFrameSize         = 4096 - headerSize
	minBufferSize        = 512
)

func defaultOptions() sessionOptions {
	return sessionOptions{
		MaxFrameSize:      8192 - headerSize,
		MaxWindowSize:     initialWindowSize,
		KeepAliveEnabled:  true,
		KeepAliveRetries:  4,
		KeepAliveInterval: 30 * time.Second,
		KeepAliveTimeout:  5 * time.Second,
		WriteTimeout:      10 * time.Second,
		AcceptBacklog:     initialAcceptBacklog,
		RecvBufferSize:    8192,
		SendBufferSize:    8192,
	}
}

// Option is session option.
type Option func(s *Session)

// DisableKeepAlive disables keep alive for a session.
func DisableKeepAlive() Option {
	return func(s *Session) {
		s.options.KeepAliveEnabled = false
	}
}

// MaxWindowSize sets receive window size for a session.
func MaxWindowSize(size int) Option {
	return func(s *Session) {
		if size < initialWindowSize {
			size = initialWindowSize
		}
		s.options.MaxWindowSize = int32(size)
	}
}

// MaxFrameSize sets max individual frame size for a session.
func MaxFrameSize(size int) Option {
	return func(s *Session) {
		if size < minFrameSize {
			size = minFrameSize
		}
		if size > maxFrameSize {
			size = maxFrameSize
		}
		s.options.MaxFrameSize = size
	}
}

// RecvBufferSize sets the read buffer size for a session.
func RecvBufferSize(size int) Option {
	return func(s *Session) {
		if size < minBufferSize {
			size = minBufferSize
		}
		s.options.RecvBufferSize = size
	}
}

// SendBufferSize sets the write buffer size for a session.
func SendBufferSize(size int) Option {
	return func(s *Session) {
		if size < minBufferSize {
			size = minBufferSize
		}
		s.options.SendBufferSize = size
	}
}

// AcceptBacklog sets the accept backlog for opened/accepted streams.
func AcceptBacklog(backlog int) Option {
	return func(s *Session) {
		if backlog > initialAcceptBacklog {
			s.options.AcceptBacklog = backlog
		}
	}
}

// KeepAlive enables keep alive and sets interval/timeout/retries for it.
func KeepAlive(interval, timeout time.Duration, retries int) Option {
	return func(s *Session) {
		s.options.KeepAliveEnabled = true
		if retries < 0 {
			retries = 0
		}
		s.options.KeepAliveRetries = retries
		if interval < time.Second {
			interval = time.Second
		}
		s.options.KeepAliveInterval = interval
		if timeout < 5*time.Millisecond {
			timeout = 5 * time.Millisecond
		}
		s.options.KeepAliveTimeout = timeout
	}
}

// SetWriteTimeout sets write timeout for a session.
func SetWriteTimeout(timeout time.Duration) Option {
	return func(s *Session) {
		s.options.WriteTimeout = timeout
	}
}
