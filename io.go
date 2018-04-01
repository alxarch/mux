package mux

import (
	"io"
	"time"
)

type writerFunc func(p []byte) (n int, err error)

func (w writerFunc) Write(p []byte) (int, error) {
	return w(p)
}

type deadlineReader interface {
	Read(p []byte) (n int, err error)
	SetReadDeadline(time.Time) error
}
type readerFunc func(p []byte) (n int, err error)

func (r readerFunc) Read(p []byte) (int, error) {
	return r(p)
}

type timeouter interface {
	Timeout() bool
}

func keepAliveReader(r io.Reader, s *Session) (io.Reader, bool) {
	conn, ok := r.(deadlineReader)
	if !ok {
		return r, false
	}

	return readerFunc(func(p []byte) (n int, err error) {
		timeout := s.options.KeepAliveInterval
		for i := -1; i < s.options.KeepAliveRetries; i++ {
			deadline := time.Now().Add(timeout)
			if err = conn.SetReadDeadline(deadline); err != nil {
				return
			}
			n, err = conn.Read(p)
			if err != nil {
				if e, ok := err.(timeouter); ok && e.Timeout() {
					timeout = s.options.KeepAliveTimeout
					go s.Ping()
					continue
				}
			}
			break
		}
		return

	}), true
}

type deadlineWriter interface {
	io.Writer
	SetWriteDeadline(time.Time) error
}

func timeoutWriter(w io.Writer, s *Session) (io.Writer, bool) {
	timeout := s.options.WriteTimeout
	if timeout <= 0 {
		return w, false
	}
	conn, ok := w.(deadlineWriter)
	if !ok {
		return w, false
	}
	return writerFunc(func(p []byte) (n int, err error) {
		deadline := time.Now().Add(timeout)
		if err = conn.SetWriteDeadline(deadline); err != nil {
			return
		}
		n, err = conn.Write(p)
		return

	}), true
}
