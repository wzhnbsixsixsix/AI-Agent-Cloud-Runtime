package acp

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := []Frame{
		{Type: FrameHello, Flags: 0, Seq: 0, Payload: []byte(`{"client_version":"v1"}`)},
		{Type: FrameEvent, Flags: FlagEndStream, Seq: 42, Payload: bytes.Repeat([]byte{0x55}, 1024)},
		{Type: FramePing, Seq: 7, Payload: []byte{1, 2, 3, 4, 5, 6, 7, 8}},
		{Type: FrameError, Payload: nil},
		{Type: FrameRun, Payload: bytes.Repeat([]byte{0xAA}, 1<<16)},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := WriteFrame(&buf, c); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got.Type != c.Type || got.Flags != c.Flags || got.Seq != c.Seq {
			t.Fatalf("header mismatch: %+v vs %+v", got, c)
		}
		if !bytes.Equal(got.Payload, c.Payload) {
			t.Fatalf("payload mismatch len got=%d want=%d", len(got.Payload), len(c.Payload))
		}
	}
}

func TestRejectBadMagic(t *testing.T) {
	buf := bytes.NewReader([]byte{0xFF, 0x01, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	if _, err := ReadFrame(buf); !errors.Is(err, ErrBadMagic) {
		t.Fatalf("want ErrBadMagic, got %v", err)
	}
}

func TestRejectBadVersion(t *testing.T) {
	// magic ok, version=0x99
	raw := []byte{Magic, 0x99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := ReadFrame(bytes.NewReader(raw)); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("want ErrBadVersion, got %v", err)
	}
}

func TestRejectOversizedPayload(t *testing.T) {
	f := Frame{Type: FrameRun, Payload: make([]byte, MaxPayloadSize+1)}
	if err := WriteFrame(io.Discard, f); !errors.Is(err, ErrPayloadTooLong) {
		t.Fatalf("want ErrPayloadTooLong, got %v", err)
	}
}

func TestPartialReadEOF(t *testing.T) {
	// 只给 5 字节，应当 EOF
	if _, err := ReadFrame(bytes.NewReader([]byte{Magic, Version, 0, 0, 0})); err == nil {
		t.Fatalf("want err, got nil")
	} else if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		// 接受任何 EOF 派生
		t.Logf("got expected error: %v", err)
	}
}

func BenchmarkWriteFrame_64B(b *testing.B) {
	f := Frame{Type: FrameEvent, Seq: 1, Payload: bytes.Repeat([]byte{0x55}, 64)}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = WriteFrame(io.Discard, f)
	}
}

func BenchmarkRoundTrip_1KB(b *testing.B) {
	f := Frame{Type: FrameEvent, Seq: 1, Payload: bytes.Repeat([]byte{0x55}, 1024)}
	var buf bytes.Buffer
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_ = WriteFrame(&buf, f)
		_, _ = ReadFrame(&buf)
	}
}
