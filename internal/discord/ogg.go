package discord

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// oggReader pulls Opus packets out of an Ogg stream (RFC 3533 / RFC 7845),
// which is what ffmpeg writes with `-c:a libopus -f ogg`. Discord takes raw
// Opus packets, one 20 ms frame each, so only the framing is undone here: no
// decoding. The two header packets (OpusHead, OpusTags) are skipped.
type oggReader struct {
	r *bufio.Reader
	// packets left from the current page
	pending [][]byte
	// a packet continued across pages
	partial []byte
	headers int
}

// maxPacket bounds one packet; a 20 ms Opus frame is at most a few KB.
const maxPacket = 64 << 10

func newOggReader(r io.Reader) *oggReader {
	return &oggReader{r: bufio.NewReaderSize(r, 32<<10)}
}

var errNotOgg = errors.New("ogg: bad capture pattern")

// Next returns the next audio packet. It returns io.EOF at the end of the
// stream.
func (o *oggReader) Next() ([]byte, error) {
	for {
		for len(o.pending) > 0 {
			p := o.pending[0]
			o.pending = o.pending[1:]
			if o.headers < 2 {
				o.headers++
				continue
			}
			return p, nil
		}
		if err := o.readPage(); err != nil {
			return nil, err
		}
	}
}

func (o *oggReader) readPage() error {
	var hdr [27]byte
	if _, err := io.ReadFull(o.r, hdr[:]); err != nil {
		// io.EOF between pages is the end; a cut-off page stays
		// io.ErrUnexpectedEOF so the player can resume the track.
		return err
	}
	if string(hdr[0:4]) != "OggS" {
		return errNotOgg
	}
	nseg := int(hdr[26])
	lacing := make([]byte, nseg)
	if _, err := io.ReadFull(o.r, lacing); err != nil {
		return truncated(err)
	}
	total := 0
	for _, l := range lacing {
		total += int(l)
	}
	body := make([]byte, total)
	if _, err := io.ReadFull(o.r, body); err != nil {
		return truncated(err)
	}
	// A continued-packet flag with nothing to continue (we joined mid
	// stream) just drops the fragment.
	if hdr[5]&0x01 == 0 {
		o.partial = nil
	}
	off := 0
	for _, l := range lacing {
		o.partial = append(o.partial, body[off:off+int(l)]...)
		off += int(l)
		if len(o.partial) > maxPacket {
			return fmt.Errorf("ogg: packet over %d bytes", maxPacket)
		}
		if l < 255 {
			if len(o.partial) > 0 {
				o.pending = append(o.pending, o.partial)
			}
			o.partial = nil
		}
	}
	return nil
}

// truncated reports a stream that ended inside a page.
func truncated(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
