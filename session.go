package mux

import (
	"bufio"
	"encoding/binary"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Session multiplexes streams over a single connection
type Session struct {
	once sync.Once
	err  error
	done chan struct{}

	client uint32
	config *Config
	conn   io.ReadWriteCloser
	pings  pings
	opened chan struct{}
	accept chan *Stream
	recv   *bufio.Reader

	streamLock sync.RWMutex
	streams    map[uint32]*Stream
	nextID     uint32

	sendLock sync.Mutex
	send     *bufio.Writer
}

// New creates a new session
func New(conn io.ReadWriteCloser, client bool, config *Config) (*Session, error) {
	if config == nil {
		config = DefaultConfig()
	} else if err := config.Verify(); err != nil {
		return nil, err
	}
	s := Session{
		done:    make(chan struct{}),
		config:  config,
		conn:    conn,
		streams: make(map[uint32]*Stream),
	}
	if client {
		s.nextID = 1
		s.client = 1
	} else {
		s.nextID = 2
	}
	s.accept = make(chan *Stream, s.config.AcceptBacklog)
	s.opened = make(chan struct{}, s.config.AcceptBacklog)
	// Set up i/o
	{
		w := timeoutWriter(conn, &s)
		s.send = bufio.NewWriterSize(w, s.config.SendBufferSize)
	}
	{
		r := timeoutReader(conn, &s)
		s.recv = bufio.NewReaderSize(r, s.config.RecvBufferSize)
	}
	go func() {
		if err := s.recvLoop(); err != nil {
			s.closeWithError(err)
		}
	}()
	if s.config.KeepAliveEnabled {
		go s.keepAlive()
	}
	return &s, nil
}

// Server creates a new server session
func Server(conn io.ReadWriteCloser, config *Config) (*Session, error) {
	return New(conn, false, config)
}

// Client creates a new server session
func Client(conn io.ReadWriteCloser, config *Config) (*Session, error) {
	return New(conn, true, config)
}

func (s *Session) flush() (err error) {
	s.sendLock.Lock()
	err = s.send.Flush()
	s.sendLock.Unlock()
	return
}

// NumStreams returns the number of streams in a session
func (s *Session) NumStreams() (n int) {
	s.streamLock.Lock()
	n = len(s.streams)
	s.streamLock.Unlock()
	return
}

func (s *Session) push(t byte, f flags, sid, size uint32) (err error) {
	h := header{}
	putFrame(h[:], t, f, sid, size)
	_, err = s.send.Write(h[:])
	return
}

func (s *Session) sendFrame(t byte, f flags, sid, size uint32) (err error) {
	h := header{}
	putFrame(h[:], t, f, sid, size)
	s.sendLock.Lock()
	s.send.Write(h[:])
	err = s.send.Flush()
	s.sendLock.Unlock()
	return
}

func (s *Session) pushData(sid uint32, data []byte) (n int, err error) {
	h := header{}
	// Split writes so big chunks don't monopolize the underlying connection
	putFrame(h[:], typeData, 0, sid, 0)
	for len(data) > 0 && err == nil {
		nn := len(data)
		if nn > s.config.MaxFrameSize {
			nn = s.config.MaxFrameSize
		}
		binary.BigEndian.PutUint32(h[8:], uint32(nn))
		s.sendLock.Lock()
		if _, err = s.send.Write(h[:]); err == nil {
			nn, err = s.send.Write(data[:nn])
		}
		s.sendLock.Unlock()
		n += nn
		data = data[nn:]
	}
	return
}

func (s *Session) generateStreamID() (sid uint32) {
	max := math.MaxUint32 - s.client
	for {
		if sid = atomic.LoadUint32(&s.nextID); sid < max {
			if atomic.CompareAndSwapUint32(&s.nextID, sid, sid+2) {
				return
			}
		} else {
			return 0
		}
	}
}

func (s *Session) sendWindowUpdate(sid uint32, delta uint32, f flags) error {
	return s.sendFrame(typeWindowUpdate, f, sid, delta)
}

// Err returns a session's close error
func (s *Session) Err() error {
	select {
	case <-s.done:
		return s.err
	default:
		return nil
	}
}

// Open opens a new stream
func (s *Session) Open() (str *Stream, err error) {
	// Wait for acks
	select {
	case s.opened <- struct{}{}:
	case <-s.done:
		return nil, ErrSessionClosed
	}
	sid := s.generateStreamID()
	if sid == 0 {
		return nil, ErrMaxOpenStreams
	}
	str = newStream(s, sid)
	if _, err = str.SendWindowUpdate(flagSYN, 0); err != nil {
		return nil, err
	}
	if err = s.putStream(str); err != nil {
		return nil, ErrSessionClosed
	}
	return str, nil
}

func (s *Session) keepAlive() (err error) {
	tick := time.NewTicker(s.config.KeepAliveInterval)
	errs := make(chan error)
	for {
		select {
		case <-s.done:
			return ErrSessionClosed
		case err = <-errs:
			s.closeWithError(err)
			return
		case <-tick.C:
			go func() {
				_, err := s.Ping()
				errs <- err
			}()
		}
	}
}
func (s *Session) recvLoop() (err error) {
	var (
		n int
		f header
	)
	for {
		_, err = io.ReadFull(s.recv, f[:])
		if err != nil {
			return
		}
		if f.Version() != protoVersion {
			return ErrInvalidFrameVersion
		}

		switch t := f.Type(); t {
		case typeGoAway:
			return s.closeWithError(ErrGoAway)
		case typePing:
			if f.Flags().is(flagACK) {
				s.pings.close(f.StreamID())
			} else {
				go s.pong(f.StreamID(), f.Size())
			}
		case typeData, typeWindowUpdate:
			var str *Stream
			ff := f.Flags()
			if ff.is(flagSYN) {
				str, _ = s.acceptStream(f.StreamID())
			} else {
				str = s.getStream(f.StreamID())
			}
			if str != nil {
				switch t {
				case typeWindowUpdate:
					n = 0
					str.send.UpdateWindow(f.Size())
				case typeData:
					nn := int64(f.Size())
					n = int(nn)
					nn, err = str.pushN(s.recv, nn)
					n -= int(nn)
					if err != nil {
						if err == ErrWindowExceeded {
							str.Close()
							err = nil
							goto discard
						}
						return
					}
				}
				if ff.is(flagACK) {
					select {
					case <-s.opened:
					default:
					}
				}
				if ff.is(flagRST) {
					str.reset()
				}
				if ff.is(flagSYN) {
					str.SendWindowUpdate(flagACK, 0)
				}
				if ff.is(flagFIN) {
					str.recv.Close()
				}
			}
		discard:
			for n > 0 {
				var dn int
				dn, err = s.recv.Discard(n)
				n -= dn
				if err != nil {
					return
				}
			}
		default:
			return ErrInvalidFrameType
		}
	}
}

// Accept blocks until a stream is opened from the other session
func (s *Session) Accept() (*Stream, error) {
	select {
	case str := <-s.accept:
		return str, nil
	case <-s.done:
		return nil, ErrSessionClosed
	}
}

func (s *Session) closeWithError(err error) (e error) {
	e = ErrSessionClosed
	s.once.Do(func() {
		s.err = err
		close(s.done)

		// Close i/o
		var streams map[uint32]*Stream
		s.sendLock.Lock()
		switch err {
		case ErrGoAway, ErrTimeout:
		default:
			s.push(typeGoAway, 0, 0, 0)
		}
		e = s.send.Flush()
		s.conn.Close()
		s.sendLock.Unlock()

		// Close streams
		s.streamLock.Lock()
		streams = s.streams
		s.streams = nil
		s.streamLock.Unlock()
		for _, str := range streams {
			if str != nil {
				str.Close()
			}
		}
	})
	return
}

// Close aborts a session
func (s *Session) Close() (err error) {
	select {
	case <-s.done:
		return s.err
	default:
		return s.closeWithError(ErrSessionClosed)
	}
}

type pings struct {
	sync.Mutex
	pings  map[uint32]chan struct{}
	pingID uint32
}

func (p *pings) new() (sid uint32, ch chan struct{}) {
	sid = atomic.AddUint32(&p.pingID, 1)
	ch = make(chan struct{})
	p.Lock()
	p.init()
	p.pings[sid] = ch
	p.Unlock()
	return
}

func (p *pings) init() {
	if p.pings == nil {
		p.pings = make(map[uint32]chan struct{})
	}
}

func (p *pings) close(sid uint32) {
	p.Lock()
	p.init()
	if ch, ok := p.pings[sid]; ok {
		delete(p.pings, sid)
		close(ch)
	}
	p.Unlock()
}

// Ping sends a ping frame to the other session and measures response time.
func (s *Session) Ping() (rtt time.Duration, err error) {
	now := time.Now()
	deadline := time.NewTimer(s.config.KeepAliveTimeout)
	defer deadline.Stop()
	sid, done := s.pings.new()
	defer s.pings.close(sid)
	if err = s.sendFrame(typePing, flagSYN, sid, 0); err != nil {
		return
	}

	select {
	case <-done:
		rtt = time.Now().Sub(now)
	case <-deadline.C:
		err = ErrTimeout
	case <-s.done:
		err = ErrSessionClosed
	}
	return
}

func (s *Session) pong(sid, size uint32) (err error) {
	return s.sendFrame(typePing, flagACK, sid, size)
}

func (s *Session) putStream(str *Stream) error {
	s.streamLock.Lock()
	if s.streams == nil {
		s.streamLock.Unlock()
		return ErrSessionClosed
	}
	s.streams[str.StreamID()] = str
	s.streamLock.Unlock()
	return nil
}

func (s *Session) isLocal(sid uint32) bool {
	return sid%2 == s.client
}
func (s *Session) acceptStream(sid uint32) (str *Stream, err error) {
	if s.isLocal(sid) {
		s.sendWindowUpdate(sid, 0, flagRST)
		return nil, ErrInvalidStreamID
	}
	str = newStream(s, sid)
	s.streamLock.Lock()
	defer s.streamLock.Unlock()
	if s.streams == nil {
		return nil, ErrSessionClosed
	}
	select {
	case <-s.done:
		str.Close()
		return nil, ErrSessionClosed
	case s.accept <- str:
		s.streams[sid] = str
		return
	default:
		// str.Close()
		s.sendWindowUpdate(sid, 0, flagRST)
		return nil, ErrAcceptBacklog
	}
}

func (s *Session) getStream(sid uint32) (str *Stream) {
	s.streamLock.RLock()
	if s.streams != nil {
		str = s.streams[sid]
	}
	s.streamLock.RUnlock()
	return
}

func (s *Session) closeStream(sid uint32) {
	s.streamLock.Lock()
	if s.streams != nil {
		if _, ok := s.streams[sid]; ok {
			delete(s.streams, sid)
		}
	}
	s.streamLock.Unlock()
}

type localAddrer interface {
	LocalAddr() net.Addr
}
type remoteAddrer interface {
	RemoteAddr() net.Addr
}

// LocalAddr returns the session's local network address if any.
func (s *Session) LocalAddr() net.Addr {
	if conn, ok := s.conn.(localAddrer); ok {
		return conn.LocalAddr()
	}
	return nil
}

// RemoteAddr returns the session's remote network address if any.
func (s *Session) RemoteAddr() net.Addr {
	if conn, ok := s.conn.(remoteAddrer); ok {
		return conn.RemoteAddr()
	}
	return nil
}
