// Package config 集中读取所有进程的环境变量。
package config

import (
	"fmt"
	"time"

	env "github.com/caarlos0/env/v10"
)

// Common 三个二进制共享的配置。
type Common struct {
	LogLevel  string `env:"LOG_LEVEL"  envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`

	RedisAddr     string `env:"REDIS_ADDR"     envDefault:"redis:6379"`
	RedisPassword string `env:"REDIS_PASSWORD"`
	RedisDB       int    `env:"REDIS_DB"       envDefault:"0"`

	PostgresDSN string  `env:"POSTGRES_DSN"`
	RAGEnabled  bool    `env:"RAG_ENABLED" envDefault:"false"`
	RAGEmbedDim int     `env:"RAG_EMBED_DIM" envDefault:"384"`
	RAGTopK     int     `env:"RAG_TOP_K" envDefault:"5"`
	RAGTenantID string  `env:"RAG_TENANT_ID" envDefault:"default"`
	RAGMinScore float64 `env:"RAG_MIN_SCORE" envDefault:"0"`

	MultiAgentEnabled   bool          `env:"MULTI_AGENT_ENABLED" envDefault:"false"`
	SubagentMaxDepth    int           `env:"SUBAGENT_MAX_DEPTH" envDefault:"2"`
	SubagentMaxChildren int           `env:"SUBAGENT_MAX_CHILDREN" envDefault:"4"`
	SubagentTimeout     time.Duration `env:"SUBAGENT_TIMEOUT" envDefault:"2m"`

	ContextCompactEnabled  bool `env:"CONTEXT_COMPACT_ENABLED" envDefault:"true"`
	ContextCompactMaxChars int  `env:"CONTEXT_COMPACT_MAX_CHARS" envDefault:"24000"`
	ContextCompactKeepHead int  `env:"CONTEXT_COMPACT_KEEP_HEAD" envDefault:"4"`
	ContextCompactKeepTail int  `env:"CONTEXT_COMPACT_KEEP_TAIL" envDefault:"8"`

	DiscoveryEnabled bool     `env:"DISCOVERY_ENABLED" envDefault:"false"`
	EtcdEndpoints    []string `env:"ETCD_ENDPOINTS" envSeparator:"," envDefault:"etcd:2379"`

	SkillServiceAddr   string        `env:"SKILL_SERVICE_ADDR" envDefault:"skilld:8084"`
	RAGServiceAddr     string        `env:"RAG_SERVICE_ADDR" envDefault:"ragd:8085"`
	HookServiceAddr    string        `env:"HOOK_SERVICE_ADDR" envDefault:"hookd:8083"`
	HookEnabled        bool          `env:"HOOK_ENABLED" envDefault:"false"`
	HookRoot           string        `env:"HOOK_ROOT" envDefault:"hooks"`
	HookFailClosed     bool          `env:"HOOK_FAIL_CLOSED" envDefault:"false"`
	HookMaxStdoutBytes int           `env:"HOOK_MAX_STDOUT_BYTES" envDefault:"65536"`
	HookTimeout        time.Duration `env:"HOOK_TIMEOUT" envDefault:"10ms"`

	OTELEnabled              bool   `env:"OTEL_ENABLED" envDefault:"true"`
	OTELServiceName          string `env:"OTEL_SERVICE_NAME"`
	OTELExporterOTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:"otel-collector:4317"`
	MetricsEnabled           bool   `env:"METRICS_ENABLED" envDefault:"true"`
	MetricsAddr              string `env:"METRICS_ADDR" envDefault:":9090"`
	MetricsPath              string `env:"METRICS_PATH" envDefault:"/metrics"`
}

// Gateway 配置。
type Gateway struct {
	Common
	GRPCAddr string `env:"GATEWAY_GRPC_ADDR" envDefault:":8080"`
	// ACPAddr 自研 ACP 协议监听地址，留空则不启动 ACP server。
	ACPAddr           string        `env:"GATEWAY_ACP_ADDR"            envDefault:":8090"`
	ACPReadTimeout    time.Duration `env:"GATEWAY_ACP_READ_TIMEOUT"    envDefault:"30s"`
	ACPMaxConnections int           `env:"GATEWAY_ACP_MAX_CONNECTIONS" envDefault:"4096"`
	ACPCacheTTL       time.Duration `env:"GATEWAY_ACP_CACHE_TTL"       envDefault:"1h"`
	// 任务总超时（gRPC 与 ACP 共用）
	RunTimeout time.Duration `env:"GATEWAY_RUN_TIMEOUT" envDefault:"10m"`
	// W3: tool 调用单次最大等待（gateway 侧；worker 侧另有 hardLimit）。
	ToolCallTimeout time.Duration `env:"GATEWAY_TOOL_CALL_TIMEOUT" envDefault:"60s"`
}

// Scheduler 配置。
type Scheduler struct {
	Common
	GRPCAddr      string `env:"SCHEDULER_GRPC_ADDR" envDefault:":8081"`
	HTTPAddr      string `env:"SCHEDULER_HTTP_ADDR" envDefault:":8082"`
	NodeID        string `env:"SCHEDULER_NODE_ID" envDefault:"scheduler-1"`
	AdvertiseAddr string `env:"SCHEDULER_ADVERTISE_ADDR" envDefault:"scheduler:8081"`
	RaftEnabled   bool   `env:"SCHEDULER_RAFT_ENABLED" envDefault:"false"`
}

// Worker 配置。
type Worker struct {
	Common
	Concurrency       int           `env:"WORKER_CONCURRENCY"       envDefault:"4"`
	ConsumerGroup     string        `env:"WORKER_CONSUMER_GROUP"    envDefault:"workers"`
	HeartbeatInterval time.Duration `env:"WORKER_HEARTBEAT_SECONDS" envDefault:"5s"`
	MaxRetry          int           `env:"WORKER_MAX_RETRY"         envDefault:"3"`
	SchedulerDial     string        `env:"SCHEDULER_DIAL_ADDR"      envDefault:"scheduler:8081"`

	LLMProvider        string        `env:"LLM_PROVIDER"            envDefault:"openai"`
	OpenAIBaseURL      string        `env:"OPENAI_BASE_URL"         envDefault:"https://open.bigmodel.cn/api/paas/v4"`
	OpenAIAPIKey       string        `env:"OPENAI_API_KEY"`
	OpenAIModel        string        `env:"OPENAI_MODEL"            envDefault:"glm-4.7-flash"`
	OpenAIMaxTokens    int           `env:"OPENAI_MAX_TOKENS"       envDefault:"65536"`
	LLMThinkingEnabled bool          `env:"LLM_THINKING_ENABLED"    envDefault:"true"`
	OpenAITimeout      time.Duration `env:"OPENAI_TIMEOUT_SECONDS"  envDefault:"60s"`

	// ---- W3: Sandbox + Tool ----
	// SandboxDriver: docker | memory | disabled。disabled 表示不启动 tool consumer。
	SandboxDriver         string        `env:"SANDBOX_DRIVER"          envDefault:"docker"`
	SandboxImage          string        `env:"SANDBOX_IMAGE"           envDefault:"alpine:3.19"`
	SandboxPoolSize       int           `env:"SANDBOX_POOL_SIZE"       envDefault:"4"`
	SandboxWorkspaceRoot  string        `env:"SANDBOX_WORKSPACE_ROOT"  envDefault:"/tmp/agentforge"`
	SandboxMemoryMB       int64         `env:"SANDBOX_MEMORY_MB"       envDefault:"256"`
	SandboxCPUQuotaUS     int64         `env:"SANDBOX_CPU_QUOTA_US"    envDefault:"50000"`
	SandboxPidsLimit      int64         `env:"SANDBOX_PIDS_LIMIT"      envDefault:"256"`
	SandboxExecHard       time.Duration `env:"SANDBOX_EXEC_HARD"       envDefault:"60s"`
	SandboxAcquireTimeout time.Duration `env:"SANDBOX_ACQUIRE_TIMEOUT" envDefault:"30s"`

	ToolConsumerGroup string   `env:"TOOL_CONSUMER_GROUP"  envDefault:"tool-runtime"`
	ToolConcurrency   int      `env:"TOOL_CONCURRENCY"     envDefault:"4"`
	ToolHTTPMaxBytes  int64    `env:"TOOL_HTTP_MAX_BYTES"  envDefault:"1048576"`
	ToolHTTPAllowList []string `env:"TOOL_HTTP_ALLOW_LIST" envSeparator:","`
	AgentToolMaxSteps int      `env:"AGENT_TOOL_MAX_STEPS" envDefault:"5"`

	// ---- W5: Skill dynamic loading ----
	SkillEnabled   bool          `env:"SKILL_ENABLED"    envDefault:"true"`
	SkillRoot      string        `env:"SKILL_ROOT"       envDefault:"skills"`
	SkillTopK      int           `env:"SKILL_TOP_K"      envDefault:"3"`
	SkillCacheTTL  time.Duration `env:"SKILL_CACHE_TTL"  envDefault:"10m"`
	SkillCacheSize int           `env:"SKILL_CACHE_SIZE" envDefault:"1024"`
}

// AgentCtl CLI 配置。
type AgentCtl struct {
	Common
	GatewayDial    string `env:"GATEWAY_DIAL_ADDR"     envDefault:"localhost:8080"`
	GatewayACPDial string `env:"GATEWAY_ACP_DIAL_ADDR" envDefault:"localhost:8090"`
	SchedulerDial  string `env:"SCHEDULER_DIAL_ADDR"   envDefault:"localhost:8081"`
	// Proto 默认使用的协议: grpc | acp
	Proto string `env:"AGENTCTL_PROTO" envDefault:"grpc"`
}

type Hook struct {
	Common
	HookGRPCAddr string `env:"HOOK_GRPC_ADDR" envDefault:":8083"`
}

type Skill struct {
	Common
	SkillGRPCAddr string `env:"SKILL_GRPC_ADDR" envDefault:":8084"`
	SkillEnabled  bool   `env:"SKILL_ENABLED" envDefault:"true"`
	SkillRoot     string `env:"SKILL_ROOT" envDefault:"skills"`
	SkillTopK     int    `env:"SKILL_TOP_K" envDefault:"3"`
}

type RAG struct {
	Common
	RAGGRPCAddr string `env:"RAG_GRPC_ADDR" envDefault:":8085"`
}

// ControlPlane 是 Web 控制台的 HTTP/BFF 配置。浏览器只访问该服务。
type ControlPlane struct {
	Common
	HTTPAddr          string `env:"CONTROLPLANE_HTTP_ADDR" envDefault:":8086"`
	GatewayDial       string `env:"GATEWAY_DIAL_ADDR" envDefault:"gateway:8080"`
	AgentDefaultImage string `env:"AGENT_DEFAULT_IMAGE" envDefault:"alpine:3.19"`
	WebDistDir        string `env:"WEB_DIST_DIR" envDefault:"/web"`
}

// LoadGateway 读取 Gateway 配置；缺失必填项 panic。
func LoadGateway() (*Gateway, error) {
	c := &Gateway{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("parse gateway env: %w", err)
	}
	return c, nil
}

// LoadScheduler 读取 Scheduler 配置。
func LoadScheduler() (*Scheduler, error) {
	c := &Scheduler{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("parse scheduler env: %w", err)
	}
	return c, nil
}

// LoadWorker 读取 Worker 配置；启用 openai 时校验 API Key。
func LoadWorker() (*Worker, error) {
	c := &Worker{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("parse worker env: %w", err)
	}
	if c.LLMProvider == "openai" && c.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required when LLM_PROVIDER=openai")
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
	return c, nil
}

// LoadAgentCtl 读取 CLI 配置。
func LoadAgentCtl() (*AgentCtl, error) {
	c := &AgentCtl{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("parse agentctl env: %w", err)
	}
	return c, nil
}

func LoadHook() (*Hook, error) {
	c := &Hook{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("parse hook env: %w", err)
	}
	return c, nil
}

func LoadSkill() (*Skill, error) {
	c := &Skill{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("parse skill env: %w", err)
	}
	return c, nil
}

func LoadRAG() (*RAG, error) {
	c := &RAG{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("parse rag env: %w", err)
	}
	return c, nil
}

func LoadControlPlane() (*ControlPlane, error) {
	c := &ControlPlane{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("parse controlplane env: %w", err)
	}
	if c.PostgresDSN == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is required for controlplane")
	}
	return c, nil
}
