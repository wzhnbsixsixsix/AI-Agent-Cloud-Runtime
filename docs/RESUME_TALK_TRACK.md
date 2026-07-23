# AgentForge 简历与面试话术

## 一句话版本

AgentForge：Go 云原生 AI Agent Runtime，支持 gRPC/ACP 流式执行、Docker L1 沙箱工具、Skill/RAG 动态上下文、本地多 Agent 编排、WASM Hook、etcd 服务发现和 Prometheus/Grafana 可观测性。

## 简历 Bullets

- 实现 Go Agent Runtime，提供 gRPC 与自研 ACP 双入口，基于 Redis Stream 完成 worker 执行、事件回放和 CLI 流式输出。
- 实现 Docker L1 sandbox tool runtime，支持预热池、bash/文件/HTTP 工具、资源限制和 OpenAI-compatible function-calling loop。
- 设计动态上下文装配链路，包含 mutable history fold、deterministic Skill selector、pgvector RAG retrieval 和 `<untrusted>` 外部内容注入。
- 实现本地 Supervisor subagent、Pipeline DAG 和自动 history compaction，用于长任务和多角色协作。
- 将 Skill/RAG/Hook 拆成独立 gRPC 服务，基于 wazero 执行 WASI Hook，并用 etcd 提供服务注册和 scheduler leader/pick 调度面。
- 接入 OpenTelemetry tracing、Prometheus metrics、Grafana dashboard 和 mock RunAgent bench，形成可复现的 runtime 可观测闭环。

## STAR 讲法

**Situation**：很多 Agent 应用把 prompt、tools、memory、RAG、orchestration 和 deployment 混在一个进程里，调试和扩展都很困难。

**Task**：做一个 runtime-first 的 Agent 平台，在保持外部执行 API 稳定的前提下，逐步加入隔离、上下文、编排、Hook、调度和可观测能力。

**Action**：用 Go 实现 Redis Streams、gRPC/ACP streaming、Docker sandbox pool、pgvector RAG、Skill selector、本地 subagents、WASM hooks、服务拆分、etcd election 和 Prometheus/Grafana。

**Result**：形成一个 W1-W10 都能 demo 的 AI Infra 项目，`agentctl run` 的公共入口保持稳定，而内部 runtime 能力逐周增强。

## 实际已实现

- gRPC `RunAgent` 和 ACP streaming path
- Redis task queue、Pub/Sub events、mutable Redis history
- Docker L1 sandbox 和内置 tools
- OpenAI-compatible / mock LLM providers
- Skill selector、pgvector RAG、Multi-Agent pipeline、context compaction
- wazero Hook service、Skill/RAG/Hook 服务拆分、etcd-backed scheduler election
- OTel tracing、Prometheus metrics、Grafana dashboard、mock RunAgent bench

## 已设计但未实现

- gVisor / Firecracker sandbox layers
- eBPF syscall audit
- CRIU checkpoint restore
- Loki / Tempo trace UI
- scheduler `Pick` 驱动的 worker-specific queue sharding
- Lark 企业中台业务集成进 mainline

## 常见面试追问

### 为什么保留 ACP，既然已经有 gRPC？

gRPC 是稳定、工程化的主入口；ACP 是协议设计和性能探索面，用来展示 framed stream、resume、协议控制和 benchmark。保留双入口能把兼容性和底层控制的取舍讲清楚。

### 为什么 W10 只交付 Docker L1 sandbox？

Docker L1 已经足够展示 tool 隔离、资源限制、预热池和 function-calling runtime。gVisor/Firecracker 是合理的安全加固方向，但会显著增加部署复杂度；W10 把它们作为 ADR 里的未来增强。

### 为什么 Skill/RAG 默认用 deterministic 方案？

作品集 demo 需要可复现。RuleSelector 和 hash embedding 不依赖外部模型 key，单测和 demo 更稳定。后续可以在相同接口后替换成 LLM selector 或真实 embedding provider。

### 下一步会做什么？

我会优先把 scheduler `Pick` 接入 worker-specific queue shard，然后补 Tempo trace UI；业务方向会单独做一个 Lark 企业研发运维中台 fork。

## 安全说法

可以说：

- “实现了 Docker L1 sandbox 和预热池。”
- “实现了 etcd-backed scheduler leader election 调度控制面。”
- “实现了 Prometheus/Grafana 可观测和 mock runtime bench。”
- “W5-W9 增强都没有破坏 `RunAgent`、ACP 和 tool 公共契约。”

不要说成已交付成果：

- “Firecracker sandbox。”
- “eBPF syscall audit。”
- “Tempo trace UI。”
- “未经实测确认的并发数字。”
- “未经当前报告确认的固定覆盖率。”

## 当前项目进度

- **W1-W10：已完成**。runtime 主链路、ACP/gRPC、Docker L1 sandbox、Skill/RAG、Multi-Agent、Hook、服务发现、调度控制面和可观测性均已交付。
- **W11：Agent Control Plane + Web Console 已完成代码实现**。新增持久 Agent Registry、Docker workspace volume、Control Plane HTTP/BFF、Redis-backed SSE 回放，以及浏览器端 Agent 创建、生命周期管理、GLM 流式运行、run 历史和只读 workspace 浏览。
- **模型配置：已统一为智谱 `glm-4.7-flash`**。通过 `.env` 的 `OPENAI_API_KEY`、`OPENAI_BASE_URL` 和 `LLM_THINKING_ENABLED` 配置；Control Plane 运行时强制所有 UI Agent 使用该模型。
- **当前验收入口：** `docs/FRONTEND_TEST_QUICKSTART.md` 提供不依赖 CLI 的前端完整功能测试步骤。

## 技术栈

| 类别 | 技术 |
|---|---|
| 后端与协议 | Go 1.22、gRPC、Protocol Buffers、自研 ACP framed TCP、HTTP REST/SSE |
| Agent Runtime | Redis Streams、Redis Pub/Sub、mutable history、function calling、Supervisor、Pipeline DAG、context compaction |
| AI 与上下文 | 智谱 GLM `glm-4.7-flash`（OpenAI-compatible API）、Skill selector、RAG、pgvector、hybrid retrieval、reranker |
| 数据与基础设施 | Redis 7、PostgreSQL 16 + pgvector、etcd、Docker Engine API、Docker Compose、Nginx |
| 安全与扩展 | Docker L1 sandbox、resource limits、read-only rootfs、WASM/WASI、wazero Hook、tool allow-list |
| 可观测性 | OpenTelemetry、Prometheus、Grafana、OTLP Collector、structured logging |
| 前端 | React 18、Vite、TypeScript、Ant Design、React Router、TanStack Query、EventSource SSE、OpenAPI TypeScript types |
| 工程化 | Make、Buf、Go test、Vitest、Docker multi-stage build、GitHub |
