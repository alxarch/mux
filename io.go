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

func timeoutReader(r io.Reader, s *Session) io.Reader {
	timeout := s.config.ReadTimeout
	if timeout <= 0 {
		return r
	}
	conn, ok := r.(deadlineReader)
	if !ok {
		return r
	}

	return readerFunc(func(p []byte) (n int, err error) {
		for {
			deadline := time.Now().Add(timeout)
			if err = conn.SetReadDeadline(deadline); err != nil {
				return
			}
			n, err = conn.Read(p)
			if err == nil {
				return
			}
			if e, ok := err.(timeouter); ok && e.Timeout() {
				go func() {
					if _, e := s.Ping(); e != nil {
						s.closeWithError(e)
					}
				}()
			} else {
				return
			}
		}

	})
}

type deadlineWriter interface {
	io.Writer
	SetWriteDeadline(time.Time) error
}

func timeoutWriter(w io.Writer, s *Session) io.Writer {
	timeout := s.config.WriteTimeout
	if timeout <= 0 {
		return w
	}
	conn, ok := w.(deadlineWriter)
	if !ok {
		return w
	}
	return writerFunc(func(p []byte) (n int, err error) {
		deadline := time.Now().Add(timeout)
		if err = conn.SetWriteDeadline(deadline); err != nil {
			return
		}
		n, err = conn.Write(p)
		return

	})
}
