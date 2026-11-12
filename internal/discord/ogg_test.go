package discord

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"testing"
)

// page builds one Ogg page holding packets (each split into 255-byte lacing
// segments). cont marks the page as continuing a packet; open leaves the last
// packet unfinished.
func page(cont bool, open bool, packets ...[]byte) []byte {
	var lacing []byte
	var body []byte
	for i, p := range packets {
		n := len(p)
		for n >= 255 {
			lacing = append(lacing, 255)
			n -= 255
		}
		if !(open && i == len(packets)-1) {
			lacing = append(lacing, byte(n))
		} else if n > 0 {
			panic("open packet must be a multiple of 255")
		}
		body = append(body, p...)
	}
	hdr := make([]byte, 27)
	copy(hdr, "OggS")
	if cont {
		hdr[5] = 1
	}
	hdr[26] = byte(len(lacing))
	out := append(hdr, lacing...)
	return append(out, body...)
}

func TestOggReaderSkipsHeadersAndJoinsPackets(t *testing.T) {
	big := bytes.Repeat([]byte{7}, 510) // spans two pages
	var stream []byte
	stream = append(stream, page(false, false, []byte("OpusHead"))...)
	stream = append(stream, page(false, false, []byte("OpusTags"))...)
	stream = append(stream, page(false, false, []byte{1, 2}, []byte{3})...)
	stream = append(stream, page(false, true, big[:255])...)
	stream = append(stream, page(true, false, big[255:], []byte{9})...)

	r := newOggReader(bytes.NewReader(stream))
	var got [][]byte
	for {
		p, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p)
	}
	if len(got) != 4 || !bytes.Equal(got[0], []byte{1, 2}) || !bytes.Equal(got[1], []byte{3}) || len(got[2]) != 510 || !bytes.Equal(got[3], []byte{9}) {
		t.Fatalf("packets = %v", got)
	}
}

func TestOggReaderRejectsGarbage(t *testing.T) {
	r := newOggReader(bytes.NewReader(bytes.Repeat([]byte("nope"), 20)))
	if _, err := r.Next(); !errors.Is(err, errNotOgg) {
		t.Fatalf("err = %v", err)
	}
}

// With ffmpeg installed, a real encode yields 20 ms frames: 1 s of audio is
// about 50 packets.
func TestOggReaderReadsFfmpegOpus(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	cmd := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-ar", "48000", "-ac", "2",
		"-c:a", "libopus", "-frame_duration", "20", "-f", "ogg", "pipe:1")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("ffmpeg without libopus: %v", err)
	}
	r := newOggReader(bytes.NewReader(out))
	n := 0
	for {
		_, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n < 48 || n > 53 {
		t.Fatalf("packets = %d, want ~50", n)
	}
}
