package mux

const protoVersion = 0
const headerSize = 12

const (
	typeData         = iota
	typeWindowUpdate = 1 << iota
	typePing
	typeGoAway
)

type flags uint16

const (
	flagSYN flags = 1 << iota
	flagACK
	flagFIN
	flagRST
)

func (f flags) is(x flags) bool {
	return f&x == x
}

type header [12]byte

func (h header) Version() byte {
	return h[0]
}
func (h header) Type() byte {
	return h[1]
}
func (h header) Flags() flags {
	return flags(h[2])<<8 | flags(h[3])
}
func (h header) StreamID() uint32 {
	return uint32(h[4])<<24 | uint32(h[5])<<16 | uint32(h[6])<<8 | uint32(h[7])
}
func (h header) Size() uint32 {
	return uint32(h[8])<<24 | uint32(h[9])<<16 | uint32(h[10])<<8 | uint32(h[11])
}
func (h *header) SetVersion(v byte) {
	h[0] = v
}
func (h *header) SetType(t byte) {
	h[1] = t
}
func (h *header) SetFlags(f flags) {
	h[2] = uint8(f << 8)
	h[3] = uint8(f)
}
func (h *header) SetStreamID(sid uint32) {
	h[4] = uint8(sid >> 24)
	h[5] = uint8(sid >> 16)
	h[6] = uint8(sid >> 8)
	h[7] = uint8(sid)
}
func (h *header) SetSize(size uint32) {
	h[8] = uint8(size >> 24)
	h[9] = uint8(size >> 16)
	h[10] = uint8(size >> 8)
	h[11] = uint8(size)
}

type frame struct {
	header
	Data []byte
}

func putFrame(b []byte, t byte, f flags, sid, size uint32) {
	_ = b[11]
	b[0] = protoVersion
	b[1] = t
	b[2] = uint8(f >> 8)
	b[3] = uint8(f)
	b[4] = uint8(sid >> 24)
	b[5] = uint8(sid >> 16)
	b[6] = uint8(sid >> 8)
	b[7] = uint8(sid)
	b[8] = uint8(size >> 24)
	b[9] = uint8(size >> 16)
	b[10] = uint8(size >> 8)
	b[11] = uint8(size)
}

func readFrame(b []byte, f *frame) {
	copy(f.header[:], b[:headerSize])
}
