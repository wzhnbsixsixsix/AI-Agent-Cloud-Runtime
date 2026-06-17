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
	GRPCAddr string `env:"SCHEDULER_GRPC_ADDR" envDefault:":8081"`
	HTTPAddr string `env:"SCHEDULER_HTTP_ADDR" envDefault:":8082"`
}

// Worker 配置。
type Worker struct {
	Common
	Concurrency       int           `env:"WORKER_CONCURRENCY"       envDefault:"4"`
	ConsumerGroup     string        `env:"WORKER_CONSUMER_GROUP"    envDefault:"workers"`
	HeartbeatInterval time.Duration `env:"WORKER_HEARTBEAT_SECONDS" envDefault:"5s"`
	MaxRetry          int           `env:"WORKER_MAX_RETRY"         envDefault:"3"`
	SchedulerDial     string        `env:"SCHEDULER_DIAL_ADDR"      envDefault:"scheduler:8081"`

	LLMProvider     string        `env:"LLM_PROVIDER"            envDefault:"openai"`
	OpenAIBaseURL   string        `env:"OPENAI_BASE_URL"         envDefault:"https://api.openai.com/v1"`
	OpenAIAPIKey    string        `env:"OPENAI_API_KEY"`
	OpenAIModel     string        `env:"OPENAI_MODEL"            envDefault:"gpt-4o-mini"`
	OpenAITimeout   time.Duration `env:"OPENAI_TIMEOUT_SECONDS"  envDefault:"60s"`

	// ---- W3: Sandbox + Tool ----
	// SandboxDriver: docker | memory | disabled。disabled 表示不启动 tool consumer。
	SandboxDriver        string        `env:"SANDBOX_DRIVER"          envDefault:"docker"`
	SandboxImage         string        `env:"SANDBOX_IMAGE"           envDefault:"alpine:3.19"`
	SandboxPoolSize      int           `env:"SANDBOX_POOL_SIZE"       envDefault:"4"`
	SandboxWorkspaceRoot string        `env:"SANDBOX_WORKSPACE_ROOT"  envDefault:"/tmp/agentforge"`
	SandboxMemoryMB      int64         `env:"SANDBOX_MEMORY_MB"       envDefault:"256"`
	SandboxCPUQuotaUS    int64         `env:"SANDBOX_CPU_QUOTA_US"    envDefault:"50000"`
	SandboxPidsLimit     int64         `env:"SANDBOX_PIDS_LIMIT"      envDefault:"256"`
	SandboxExecHard      time.Duration `env:"SANDBOX_EXEC_HARD"       envDefault:"60s"`
	SandboxAcquireTimeout time.Duration `env:"SANDBOX_ACQUIRE_TIMEOUT" envDefault:"30s"`

	ToolConsumerGroup string   `env:"TOOL_CONSUMER_GROUP"  envDefault:"tool-runtime"`
	ToolConcurrency   int      `env:"TOOL_CONCURRENCY"     envDefault:"4"`
	ToolHTTPMaxBytes  int64    `env:"TOOL_HTTP_MAX_BYTES"  envDefault:"1048576"`
	ToolHTTPAllowList []string `env:"TOOL_HTTP_ALLOW_LIST" envSeparator:","`
	AgentToolMaxSteps int      `env:"AGENT_TOOL_MAX_STEPS" envDefault:"5"`
}

// AgentCtl CLI 配置。
type AgentCtl struct {
	Common
	GatewayDial    string `env:"GATEWAY_DIAL_ADDR"     envDefault:"localhost:8080"`
	GatewayACPDial string `env:"GATEWAY_ACP_DIAL_ADDR" envDefault:"localhost:8090"`
	// Proto 默认使用的协议: grpc | acp
	Proto string `env:"AGENTCTL_PROTO" envDefault:"grpc"`
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
