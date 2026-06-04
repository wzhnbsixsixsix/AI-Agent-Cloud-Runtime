package obs

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(rand.Reader, 0)
)

// NewTraceID 返回一个 ULID 字符串，单调递增。
// 也用于 RunID 生成。
func NewTraceID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}

// NewRunID 与 NewTraceID 同源；语义化别名。
func NewRunID() string { return NewTraceID() }
