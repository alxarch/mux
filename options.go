package mux

import (
	"errors"
	"time"
)

// Config is the configuration for a Session.
type Config struct {
	// MaxFrameSize sets the maximum size for frames.
	MaxFrameSize int
	// MaxWindowSize sets the maximum window size for new streams.
	MaxWindowSize int32
	// KeepAliveEnabled enables periodic keep alive pings.
	KeepAliveEnabled bool
	// KeepAliveInterval sets keep alive ping interval.
	KeepAliveInterval time.Duration
	// KeepAliveTimeout sets keep alive ping timeout.
	KeepAliveTimeout time.Duration
	// AcceptBacklog sets the max number of queued accept/open streams.
	// An opened stream is queued until it receives a frame with ACK flag set.
	// An accepted stream is queued until it is returned from Accept() method.
	AcceptBacklog int
	// RecvBufferSize sets the receive buffer size.
	RecvBufferSize int
	// SendBufferSize sets the send buffer size.
	SendBufferSize int
	// ReadTimeout sets the read timeout.
	// If the session's conn has a SetReadDeadline(time.Time)error method
	// the session will set the deadline before every read,
	// send a ping if a timeout error occurs to verify connection's health and
	// close itself if the ping fails.
	ReadTimeout time.Duration
	// WriteTimeout sets the write timeout.
	// If the session's conn has a SetWriteDeadline(time.Time)error method
	// the session will set the deadline before every write and close itself
	// if a timeout occurs.
	WriteTimeout time.Duration
}

const (
	initialAcceptBacklog = 16
	maxFrameSize         = 1<<16 - headerSize
	minBufferSize        = 256
)

// Verify checks for configuration errors.
func (c *Config) Verify() error {
	if c.AcceptBacklog < initialAcceptBacklog {
		return errors.New("Invalid accept backlog")
	}
	if c.MaxWindowSize < initialWindowSize {
		return errors.New("Invalid window size")
	}
	if c.KeepAliveEnabled {
		if c.KeepAliveInterval < 1 {
			return errors.New("Invalid keep alive interval")
		}
		if c.KeepAliveTimeout < 1 {
			return errors.New("Invalid keep alive timeout")
		}
	}
	if c.MaxFrameSize < (minBufferSize - headerSize) {
		return errors.New("Invalid max frame size")
	}
	if c.SendBufferSize < minBufferSize {
		return errors.New("Invalid send buffer size")
	}
	if c.RecvBufferSize < minBufferSize {
		return errors.New("Invalid receive buffer size")
	}
	return nil
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxFrameSize:      8192 - headerSize,
		MaxWindowSize:     initialWindowSize,
		KeepAliveEnabled:  false,
		KeepAliveInterval: 30 * time.Second,
		KeepAliveTimeout:  5 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       30 * time.Second,
		AcceptBacklog:     initialAcceptBacklog,
		RecvBufferSize:    8192,
		SendBufferSize:    8192,
	}
}
