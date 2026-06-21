package obs

// Counter / Histogram 是 Metrics 的 noop 抽象。
// W9 接入 Prometheus 时换成真正实现，调用方零感知。
type Counter interface {
	Inc(labels ...string)
	Add(v float64, labels ...string)
}

type Histogram interface {
	Observe(v float64, labels ...string)
}

type noopCounter struct{}

func (noopCounter) Inc(...string)          {}
func (noopCounter) Add(float64, ...string) {}

type noopHistogram struct{}

func (noopHistogram) Observe(float64, ...string) {}

// NewCounter / NewHistogram 当前均返回 noop。
func NewCounter(name string, labels ...string) Counter     { return noopCounter{} }
func NewHistogram(name string, labels ...string) Histogram { return noopHistogram{} }
