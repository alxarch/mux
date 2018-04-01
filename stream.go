package mux

import (
	"bytes"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const initialWindowSize = 256 * 1024

// Stream is a multiplexed net.Conn
type Stream struct {
	once sync.Once

	recvWindow int32
	recvDelta  int32
	recv       recvStream
	send       sendStream
}

func newStream(session *Session, sid uint32) *Stream {
	str := Stream{
		send: sendStream{
			sid:     sid,
			session: session,
			window:  initialWindowSize,
		},
		recvWindow: initialWindowSize,
		recvDelta:  session.options.MaxWindowSize - initialWindowSize,
	}
	str.send.state |= stateWritable
	return &str
}

// Session returns the stream's session.
func (s *Stream) Session() *Session {
	return s.send.session
}

// StreamID returns the stream's id.
func (s *Stream) StreamID() uint32 {
	return s.send.sid
}

func (s *Stream) reset() (err error) {
	err = ErrStreamClosed
	s.once.Do(func() {
		err = nil
		s.recv.Close()
		s.send.Close()
		s.Session().closeStream(s.StreamID())
	})
	return
}

// Close closes the stream and sends a RST window update
func (s *Stream) Close() (err error) {
	err = ErrStreamClosed
	s.once.Do(func() {
		s.recv.Close()
		s.send.Close()

		s.Session().closeStream(s.StreamID())
		go s.Session().sendWindowUpdate(s.StreamID(), 0, flagRST)
		err = nil
	})
	return
}

var _ net.Conn = new(Stream)

// SetDeadline implements the net.Conn interface
func (s *Stream) SetDeadline(deadline time.Time) error {
	rerr := s.SetReadDeadline(deadline)
	werr := s.SetWriteDeadline(deadline)
	if rerr != nil && werr != nil {
		return ErrStreamClosed
	}
	return nil
}

// LocalAddr implements the net.Conn interface
func (s *Stream) LocalAddr() net.Addr {
	return s.Session().LocalAddr()
}

// RemoteAddr implements the net.Conn interface
func (s *Stream) RemoteAddr() net.Addr {
	return s.Session().RemoteAddr()
}

// Write implements the io.Writer interface
func (s *Stream) Write(p []byte) (n int, err error) {
	return s.send.Write(p)
}

// // CloseRead closes a stream's reader
// func (s *Stream) CloseRead() (err error) {
// 	return s.recv.Close()
// }

// CloseWrite performs a half close for the stream and sends a FIN window update
func (s *Stream) CloseWrite() (err error) {
	if err = s.send.Close(); err == nil {
		err = s.Session().sendWindowUpdate(s.StreamID(), 0, flagFIN)
	}
	return
}

// updateRecvWindowDelta atomicaly updates recv delta and checks if a window update is needed
func (s *Stream) updateRecvWindowDelta(d int32) (delta int32) {
	max := atomic.LoadInt32(&s.recvWindow)/2 - d
	for {
		delta = atomic.LoadInt32(&s.recvDelta)
		switch {
		case delta > max:
			// delta will be more than half the recv window, send an update
			if atomic.CompareAndSwapInt32(&s.recvDelta, delta, 0) {
				return delta + d
			}
		default:
			if atomic.CompareAndSwapInt32(&s.recvDelta, delta, delta+d) {
				return 0
			}
		}
	}
}

// SendWindowUpdate updates recv window delta and sends a window update frame
// if flags are provided or delta is more than half of receive window
func (s *Stream) SendWindowUpdate(f flags, delta int32) (w int32, err error) {
	delta = s.updateRecvWindowDelta(delta)
	switch {
	case delta > 0:
		err = s.Session().sendWindowUpdate(s.StreamID(), uint32(delta), f)
		if err == nil {
			w = atomic.AddInt32(&s.recvWindow, delta)
		} else {
			atomic.AddInt32(&s.recvDelta, delta)
		}
	case f != 0:
		err = s.Session().sendWindowUpdate(s.StreamID(), 0, f)
	}
	return
}

func (s *Stream) Read(p []byte) (n int, err error) {
	n, err = s.recv.Read(p)
	if n > 0 {
		s.SendWindowUpdate(0, int32(n))
	}
	return
}

func (s *Stream) pushN(src io.Reader, n int64) (int64, error) {
	if n > 0 && atomic.AddInt32(&s.recvWindow, int32(-n)) < 0 {
		return 0, ErrWindowExceeded
	}
	lim := io.LimitedReader{
		N: n,
		R: src,
	}
	return s.recv.PushFrom(&lim)
}

// SetReadDeadline implements net.Conn inteface.
func (s *Stream) SetReadDeadline(deadline time.Time) error {
	return s.send.setDeadline(deadline)
}

// SetWriteDeadline implements net.Conn inteface.
func (s *Stream) SetWriteDeadline(deadline time.Time) error {
	return s.send.setDeadline(deadline)
}

type baseStream struct {
	sync.Mutex
	cond     sync.Cond
	state    state
	deadline *time.Timer
}

func (r *baseStream) unsetDeadline() {
	// Unset stateTimeout if deadline had fired
	if r.deadline != nil && !r.deadline.Stop() {
		r.state &^= stateTimeout
	}
}
func (r *baseStream) init() {
	if r.cond.L == nil {
		r.cond.L = r
	}
	return
}

func (r *baseStream) setTimeout(timeout time.Duration) error {
	if r.state.IsClosed() {
		return ErrStreamClosed
	}

	// Initialize cond
	r.init()

	switch {
	case r.deadline == nil:
		// Create the deadline timer
		r.deadline = time.AfterFunc(timeout, func() {
			r.Lock()
			r.state |= stateTimeout
			r.Unlock()
			r.cond.Broadcast()
		})
	case !r.deadline.Stop():
		// Deadline has fired, unset stateTimeout
		r.state &^= stateTimeout
		fallthrough
	default:
		// Reset the deadline timer
		r.deadline.Reset(timeout)
	}
	return nil
}

func (r *baseStream) setDeadline(deadline time.Time) error {
	r.Lock()
	defer r.Unlock()

	if r.state.IsClosed() {
		return ErrStreamClosed
	}
	if deadline.IsZero() {
		r.unsetDeadline()
		return nil
	}
	return r.setTimeout(time.Until(deadline))
}

func (r *baseStream) Close() (err error) {
	r.Lock()
	if r.state.IsClosed() {
		r.Unlock()
		err = ErrStreamClosed
		return
	}
	r.unsetDeadline()
	r.state |= stateClosed
	// Notify all Read/Write() that state has changed
	r.init()
	r.cond.Broadcast()
	r.Unlock()
	return
}

type recvStream struct {
	baseStream
	buffer bytes.Buffer
}

// PushFrom pushes all src data to the stream buffer
func (r *recvStream) PushFrom(src io.Reader) (n int64, err error) {
	r.Lock()
	if r.state.IsClosed() {
		r.Unlock()
		err = ErrStreamClosed
		return
	}
	n, err = r.buffer.ReadFrom(src)
	if n > 0 {
		r.state |= stateReadable
		r.cond.L = r
		// Notify all Read() that stream is readable
		r.cond.Broadcast()
	}
	r.Unlock()
	return
}

// Push writes a chunk into the buffer and unblocks all reads
func (r *recvStream) Push(p []byte) (n int, err error) {
	r.Lock()
	if r.state.IsClosed() {
		r.Unlock()
		err = ErrStreamClosed
		return
	}

	n, err = r.buffer.Write(p)
	if n > 0 {
		r.state |= stateReadable
		r.init()
	}
	r.Unlock()

	// Signal outside the lock
	if n > 0 {
		// Notify Read() that data is available for reading
		r.cond.Broadcast()
	}
	return
}

func (r *recvStream) Read(p []byte) (n int, err error) {
	r.Lock()
	r.init()
	for r.state == 0 {
		r.cond.Wait()
	}
	if r.state.Readable() {
		n, err = r.buffer.Read(p)
		if r.buffer.Len() == 0 {
			r.state &^= stateReadable
		}
	} else if err = r.state.Err(); err == ErrStreamClosed {
		err = io.EOF
	}
	r.Unlock()
	return
}

type sendStream struct {
	baseStream
	sid     uint32
	window  uint32
	session *Session
}

func (s *sendStream) UpdateWindow(delta uint32) uint32 {
	s.Lock()
	s.window += delta
	delta = s.window
	s.Unlock()
	s.cond.Broadcast()
	return delta
}

func (s *sendStream) Write(p []byte) (n int, err error) {
	s.Lock()
	s.init()
	for s.state == 0 {
		s.cond.Wait()
	}
	if err = s.state.Err(); err != nil {
		s.Unlock()
		return
	}
	// Lock other Write() so all needed frames are pushed in sequence
	s.state &^= stateWritable
	for len(p) > 0 && err == nil {
		for s.state == 0 && s.window < 1 {
			s.cond.Wait()
		}
		if err = s.state.Err(); err != nil {
			s.Unlock()
			return
		}

		nn := len(p)
		w := uint32(nn)
		if s.window < w {
			w = s.window
			s.window = 0
		} else {
			s.window -= w
		}
		nn, err = s.session.pushData(s.sid, p[:w])
		p = p[nn:]
		n += nn
	}
	s.state |= stateWritable
	s.cond.Broadcast()
	s.Unlock()
	if err == nil {
		err = s.session.flush()
	}
	return
}

type state uint32

// stream states
const (
	stateReadable = 1 << iota
	stateWritable
	stateClosed
	stateTimeout
)

func (s state) Readable() bool {
	return s&stateReadable == stateReadable
}
func (s state) Writable() bool {
	return s&stateWritable == stateWritable
}
func (s state) IsClosed() bool {
	return s&stateClosed == stateClosed
}

func (s state) Err() error {
	switch {
	// case s&stateEOF == stateEOF:
	// 	return io.EOF
	case s&stateClosed == stateClosed:
		return ErrStreamClosed
	case s&stateTimeout == stateTimeout:
		return ErrTimeout
	default:
		return nil
	}
}
