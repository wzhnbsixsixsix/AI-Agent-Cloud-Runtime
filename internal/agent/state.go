// Package agent 提供 Run 状态机与 Runner（worker 内的执行内核）。
package agent

import "fmt"

// State Run 的生命周期状态。
type State string

const (
	StatePending     State = "PENDING"
	StateRunning     State = "RUNNING"
	StateWaitingTool State = "WAITING_TOOL"
	StateCompacting  State = "COMPACTING"
	StateDone        State = "DONE"
	StateFailed      State = "FAILED"
)

// validTransitions 状态合法迁移表。
var validTransitions = map[State]map[State]bool{
	StatePending: {
		StateRunning: true,
		StateFailed:  true,
	},
	StateRunning: {
		StateWaitingTool: true,
		StateCompacting:  true,
		StateDone:        true,
		StateFailed:      true,
	},
	StateWaitingTool: {
		StateRunning: true,
		StateFailed:  true,
	},
	StateCompacting: {
		StateRunning: true,
		StateFailed:  true,
	},
}

// CanTransit 判断 from -> to 是否合法。
func CanTransit(from, to State) bool {
	if from == to {
		return false
	}
	if m, ok := validTransitions[from]; ok && m[to] {
		return true
	}
	return false
}

// MustTransit 校验失败返回 error，方便上层短路。
func MustTransit(from, to State) error {
	if !CanTransit(from, to) {
		return fmt.Errorf("invalid state transition: %s -> %s", from, to)
	}
	return nil
}
