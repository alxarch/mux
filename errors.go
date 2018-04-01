package mux

// Error is an error code implementing net.Error interface.
type Error uint8

// Error implements error interface.
func (e Error) Error() string {
	switch e {
	case ErrSessionClosed:
		return "Session closed"
	case ErrStreamClosed:
		return "Stream closed"
	case ErrTimeout:
		return "Timeout"
	case ErrGoAway:
		return "GoAway"
	case ErrAcceptBacklog:
		return "Accept backlog exceeded"
	case ErrWindowExceeded:
		return "Window exceeded"
	case ErrInvalidFrameVersion:
		return "Invalid frame version"
	case ErrInvalidFrameType:
		return "Invalid frame type"
	case ErrMaxOpenStreams:
		return "Max open streams"
	default:
		return "Unknown error"
	}
}

// Temporary implements net.Error interface
func (Error) Temporary() bool {
	return false
}

// Timeout implements net.Error interface
func (e Error) Timeout() bool {
	return e == ErrTimeout
}

const (
	// ErrStreamClosed is returned when a stream is closed.
	ErrStreamClosed Error = iota
	// ErrSessionClosed is returned when a session is closed.
	ErrSessionClosed
	// ErrGoAway is returned when a session received a GoAway frame.
	ErrGoAway
	// ErrTimeout is returned when an i/o op failed with a timeout.
	ErrTimeout
	// ErrInvalidFrameVersion is returned when session received a frame with invalid protocol version.
	ErrInvalidFrameVersion
	// ErrInvalidFrameType is returned when session received a frame with invalid frame type.
	ErrInvalidFrameType
	// ErrInvalidStreamID is returned when session received a SYN frame with invalid stream id.
	ErrInvalidStreamID
	// ErrMaxOpenStreams is returned when session exceeds AcceptBacklog when opening a stream without receiving any ACK frames.
	ErrMaxOpenStreams
	// ErrAcceptBacklog is returned when session exceeds AcceptBacklog when accepting a stream.
	ErrAcceptBacklog
	// ErrWindowExceeded is returned when stream receives more bytes than it's current receive window.
	ErrWindowExceeded
)
