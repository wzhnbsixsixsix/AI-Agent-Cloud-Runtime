package acp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// 错误集合。decode 错误等价于"协议被破坏"，调用方应直接关连接。
var (
	ErrBadMagic       = errors.New("acp: bad magic")
	ErrBadVersion     = errors.New("acp: unsupported version")
	ErrPayloadTooLong = errors.New("acp: payload too long")
)

// WriteFrame 把一帧二进制写入 w。
//
// 性能注意：使用单个临时 buffer 一次性 Write，避免多次 syscall。
// payload 直接引用，不做 copy。
func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > MaxPayloadSize {
		return ErrPayloadTooLong
	}
	// 头部 + varint(<=10) + payload，一次性 buffer
	buf := make([]byte, HeaderFixedSize+binary.MaxVarintLen64+len(f.Payload))
	buf[0] = Magic
	buf[1] = Version
	buf[2] = byte(f.Type)
	buf[3] = byte(f.Flags)
	binary.BigEndian.PutUint64(buf[4:12], f.Seq)
	n := binary.PutUvarint(buf[HeaderFixedSize:], uint64(len(f.Payload)))
	off := HeaderFixedSize + n
	copy(buf[off:], f.Payload)
	off += len(f.Payload)
	if _, err := w.Write(buf[:off]); err != nil {
		return err
	}
	return nil
}

// ReadFrame 从 r 解析一帧；返回的 Frame.Payload 是新分配的切片。
//
// r 必须可以多次小块读（一般是 *bufio.Reader 或 net.Conn）。
func ReadFrame(r io.Reader) (Frame, error) {
	var head [HeaderFixedSize]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Frame{}, err
	}
	if head[0] != Magic {
		return Frame{}, fmt.Errorf("%w: got 0x%02x", ErrBadMagic, head[0])
	}
	if head[1] != Version {
		return Frame{}, fmt.Errorf("%w: got 0x%02x", ErrBadVersion, head[1])
	}
	f := Frame{
		Type:  FrameType(head[2]),
		Flags: Flags(head[3]),
		Seq:   binary.BigEndian.Uint64(head[4:12]),
	}

	// uvarint 长度需要字节流式读取
	length, err := readUvarint(r)
	if err != nil {
		return Frame{}, fmt.Errorf("read len: %w", err)
	}
	if length > MaxPayloadSize {
		return Frame{}, fmt.Errorf("%w: %d", ErrPayloadTooLong, length)
	}
	if length > 0 {
		f.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return Frame{}, fmt.Errorf("read payload: %w", err)
		}
	}
	return f, nil
}

// readUvarint 从一个普通 io.Reader 读 uvarint，避免要求传入 ByteReader。
func readUvarint(r io.Reader) (uint64, error) {
	var (
		x   uint64
		s   uint
		buf [1]byte
	)
	for i := 0; i < binary.MaxVarintLen64; i++ {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, err
		}
		b := buf[0]
		if b < 0x80 {
			if i == binary.MaxVarintLen64-1 && b > 1 {
				return 0, fmt.Errorf("acp: uvarint overflow")
			}
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, fmt.Errorf("acp: uvarint too long")
}
