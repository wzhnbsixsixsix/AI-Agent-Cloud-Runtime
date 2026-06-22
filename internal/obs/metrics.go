package obs

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Counter interface {
	Inc(labels ...string)
	Add(v float64, labels ...string)
}

type Histogram interface {
	Observe(v float64, labels ...string)
}

type promCounter struct{ vec *prometheus.CounterVec }

func (c promCounter) Inc(labels ...string)            { c.vec.WithLabelValues(labels...).Inc() }
func (c promCounter) Add(v float64, labels ...string) { c.vec.WithLabelValues(labels...).Add(v) }

type promHistogram struct{ vec *prometheus.HistogramVec }

func (h promHistogram) Observe(v float64, labels ...string) {
	h.vec.WithLabelValues(labels...).Observe(v)
}

func NewCounter(name string, labels ...string) Counter {
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: name,
		Help: name,
	}, labels)
	return promCounter{vec: registerCounterVec(vec)}
}

func NewHistogram(name string, labels ...string) Histogram {
	vec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    name,
		Help:    name,
		Buckets: prometheus.DefBuckets,
	}, labels)
	return promHistogram{vec: registerHistogramVec(vec)}
}

func registerCounterVec(vec *prometheus.CounterVec) *prometheus.CounterVec {
	if err := prometheus.Register(vec); err != nil {
		if already, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := already.ExistingCollector.(*prometheus.CounterVec); ok {
				return existing
			}
		}
	}
	return vec
}

func registerHistogramVec(vec *prometheus.HistogramVec) *prometheus.HistogramVec {
	if err := prometheus.Register(vec); err != nil {
		if already, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := already.ExistingCollector.(*prometheus.HistogramVec); ok {
				return existing
			}
		}
	}
	return vec
}

var serviceName atomic.Value

func SetServiceName(name string) {
	if name == "" {
		name = "agentforge"
	}
	serviceName.Store(name)
}

func ServiceName() string {
	if v, ok := serviceName.Load().(string); ok && v != "" {
		return v
	}
	return "agentforge"
}

var (
	RunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentforge_runs_total",
		Help: "Total agent runs by service and status.",
	}, []string{"service", "status"})
	RunDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_run_duration_seconds",
		Help:    "Agent run duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.05, 2, 12),
	}, []string{"service", "status"})
	RunTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentforge_run_tokens_total",
		Help: "Total streamed tokens by service.",
	}, []string{"service"})
	LLMStreamDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_llm_stream_duration_seconds",
		Help:    "LLM streaming call duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.05, 2, 12),
	}, []string{"service", "provider", "status"})
	ToolTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentforge_tool_calls_total",
		Help: "Total tool calls by tool and status.",
	}, []string{"service", "tool", "status"})
	ToolDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_tool_duration_seconds",
		Help:    "Tool execution duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 14),
	}, []string{"service", "tool", "status"})
	HookTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentforge_hook_calls_total",
		Help: "Total hook calls by event and status.",
	}, []string{"service", "event", "status"})
	HookDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_hook_duration_seconds",
		Help:    "Hook execution duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
	}, []string{"service", "event", "status"})
	RAGDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_rag_duration_seconds",
		Help:    "RAG retrieval duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.005, 2, 12),
	}, []string{"service", "status"})
	RAGResults = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_rag_results",
		Help:    "Number of RAG chunks returned.",
		Buckets: []float64{0, 1, 2, 3, 5, 8, 13, 21},
	}, []string{"service"})
	SkillDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_skill_select_duration_seconds",
		Help:    "Skill selection duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
	}, []string{"service", "status"})
	SkillSelected = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_skill_selected",
		Help:    "Number of selected skills.",
		Buckets: []float64{0, 1, 2, 3, 5, 8},
	}, []string{"service"})
	SchedulerPickDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_scheduler_pick_duration_seconds",
		Help:    "Scheduler Pick RPC duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
	}, []string{"service", "status"})
	SchedulerLiveWorkers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentforge_scheduler_live_workers",
		Help: "Live workers visible to scheduler.",
	}, []string{"service"})
	QueueEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "agentforge_queue_events_total",
		Help: "Queue and pubsub events by kind and status.",
	}, []string{"service", "kind", "status"})
	SandboxAcquireDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentforge_sandbox_acquire_duration_seconds",
		Help:    "Sandbox acquire duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 14),
	}, []string{"service", "status"})
	SandboxPoolReady = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentforge_sandbox_pool_ready",
		Help: "Ready sandboxes in the pool.",
	}, []string{"service", "driver"})
	SandboxInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentforge_sandbox_in_flight",
		Help: "Acquired sandboxes not yet released.",
	}, []string{"service", "driver"})
)

func ObserveDuration(hist *prometheus.HistogramVec, start time.Time, labels ...string) {
	hist.WithLabelValues(labels...).Observe(time.Since(start).Seconds())
}

func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
