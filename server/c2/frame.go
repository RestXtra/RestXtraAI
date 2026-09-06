package c2

import (
	"encoding/binary"
	"io"
	"sync"
)

// Tunnel framing between the teamserver and the beacon over a held TCP link.
//
//	header: [4-byte length][1-byte type][4-byte stream-id]  then payload
//	type 0 = dial   (payload: target address / ack "ok"|"err:...")
//	type 1 = data   (payload: bytes)
//	type 2 = close
const (
	frameDial  = 0
	frameData  = 1
	frameClose = 2
)

func writeFrame(w io.Writer, typ byte, stream uint32, payload []byte) error {
	var hdr [9]byte
	binary.BigEndian.PutUint32(hdr[:4], uint32(len(payload)))
	hdr[4] = typ
	binary.BigEndian.PutUint32(hdr[5:9], stream)
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

func readFrame(r io.Reader) (typ byte, stream uint32, payload []byte, err error) {
	var hdr [9]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	ln := binary.BigEndian.Uint32(hdr[:4])
	typ = hdr[4]
	stream = binary.BigEndian.Uint32(hdr[5:9])
	if ln > 0 {
		payload = make([]byte, ln)
		if _, err = io.ReadFull(r, payload); err != nil {
			return
		}
	}
	return
}

// beaconLink wraps the teamserver<->beacon TCP link with a writer mutex and a
// per-stream frame dispatcher so multiple concurrent SOCKS streams can share one
// link.
type beaconLink struct {
	conn    io.ReadWriteCloser
	wmu     sync.Mutex
	mu      sync.Mutex
	streams map[uint32]chan framed
	nextID  uint32
}

type framed struct {
	typ     byte
	payload []byte
}

func newBeaconLink(conn io.ReadWriteCloser) *beaconLink {
	b := &beaconLink{
		conn:    conn,
		streams: make(map[uint32]chan framed),
		nextID:  1,
	}
	go b.reader()
	return b
}

func (b *beaconLink) write(typ byte, stream uint32, payload []byte) error {
	b.wmu.Lock()
	defer b.wmu.Unlock()
	return writeFrame(b.conn, typ, stream, payload)
}

func (b *beaconLink) newStream() (uint32, chan framed) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	ch := make(chan framed, 32)
	b.streams[id] = ch
	return id, ch
}

func (b *beaconLink) closeStream(stream uint32) {
	b.mu.Lock()
	if ch, ok := b.streams[stream]; ok {
		delete(b.streams, stream)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *beaconLink) reader() {
	for {
		typ, stream, payload, err := readFrame(b.conn)
		if err != nil {
			b.closeAll()
			return
		}
		b.mu.Lock()
		ch, ok := b.streams[stream]
		b.mu.Unlock()
		if ok {
			select {
			case ch <- framed{typ: typ, payload: payload}:
			default: // drop if congested
			}
		}
	}
}

func (b *beaconLink) closeAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, ch := range b.streams {
		delete(b.streams, id)
		close(ch)
	}
}
