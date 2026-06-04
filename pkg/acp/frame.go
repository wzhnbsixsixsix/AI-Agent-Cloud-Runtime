// Package acp 实现 AgentForge 自研的 Agent Control Protocol（v1）。
//
// 详见 ./spec.md。本文件定义帧结构与常量。
package acp

// Magic / Version 用于帧头第 0、1 字节。
const (
	Magic   byte = 0xAC
	Version byte = 0x01
)

// FrameType 帧类型枚举。
type FrameType uint8

// 帧类型常量，对齐 spec.md。
const (
	FrameHello    FrameType = 0x01
	FrameHelloAck FrameType = 0x02
	FrameRun      FrameType = 0x10
	FrameEvent    FrameType = 0x20
	FramePing     FrameType = 0x30
	FramePong     FrameType = 0x31
	FrameResume   FrameType = 0x40
	FrameClose    FrameType = 0x50
	FrameError    FrameType = 0xF0
)

// String 给日志与 benchmark 输出友好的名字。
func (t FrameType) String() string {
	switch t {
	case FrameHello:
		return "HELLO"
	case FrameHelloAck:
		return "HELLO_ACK"
	case FrameRun:
		return "RUN"
	case FrameEvent:
		return "EVENT"
	case FramePing:
		return "PING"
	case FramePong:
		return "PONG"
	case FrameResume:
		return "RESUME"
	case FrameClose:
		return "CLOSE"
	case FrameError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Flags 帧标志位。
type Flags uint8

// 标志位常量。
const (
	FlagEndStream Flags = 0x01
	FlagReserved  Flags = 0x02
)

// Has 判断 flag 是否被置位。
func (f Flags) Has(other Flags) bool { return f&other == other }

// MaxPayloadSize 最大 payload 长度（16 MiB），超限直接关连接。
const MaxPayloadSize = 16 * 1024 * 1024

// HeaderFixedSize 头部固定段长度：magic(1)+ver(1)+type(1)+flags(1)+seq(8)。
// 后续紧跟 uvarint(len)。
const HeaderFixedSize = 12

// Frame 是一帧 ACP 数据的内存表示。
type Frame struct {
	Type    FrameType
	Flags   Flags
	Seq     uint64
	Payload []byte
}
