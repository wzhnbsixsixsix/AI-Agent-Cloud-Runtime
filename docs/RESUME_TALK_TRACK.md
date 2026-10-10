# AgentForge 简历与面试话术

## 一句话版本

AgentForge：Go 云原生 AI Agent Runtime 与 Web Control Plane，支持 Agent 注册和持久 workspace、gRPC/ACP 流式执行、Docker L1 沙箱工具、Skill/RAG 动态上下文、本地多 Agent 编排、WASM Hook、etcd 服务发现及 Prometheus/Grafana 可观测性。

## 简历 LaTeX 版本（截至 2026-08-05）

```latex
\begin{onecolentry}
  \textbf{项目简介：} 基于 Go 构建的云原生 AI Agent Runtime 与 Web Control Plane，覆盖 Agent 注册、任务提交、流式执行、工具隔离、动态上下文、智能体编排与可观测性；支持 gRPC / 自研 ACP 双协议入口及 Docker Compose 一键部署。

  \textbf{核心技术栈：} Go, gRPC, Redis Streams/PubSub, PostgreSQL/pgvector, Docker, React/TypeScript, WASM/Wazero, etcd, OpenTelemetry, Prometheus, Grafana

  \begin{highlights}
  \item \textbf{分布式流式执行与控制平面：}设计 \texttt{Gateway--Redis Stream--Worker} 异步执行链路，通过 Consumer Group 消费任务、Pub/Sub 回传 Token 与工具事件；实现 gRPC、ACP framed TCP 双入口及断线续传，并构建 React Control Plane，提供 Agent 注册、生命周期管理、SSE 回放、活跃 Run 刷新恢复与实时依赖健康状态。
  \item \textbf{持久化 Agent 与安全工具运行时：}通过 Docker Engine API 创建具备独立 named volume 的持久 Agent workspace，使跨工具调用与跨 Run 文件状态可复用；构建 Docker L1 Sandbox 预热池，配置只读根文件系统、网络隔离、Capabilities 收敛及 CPU/内存/PID 限制，接入 OpenAI-compatible Function Calling，支持 bash、文件读写与 HTTP 工具调用。
  \item \textbf{上下文、编排与平台扩展：}实现 mutable history fold、Skill 动态选择、pgvector Hybrid RAG 与 \texttt{<untrusted>} 上下文注入，以及 Supervisor Subagent、Pipeline DAG 和自动上下文压缩；将 Skill/RAG/Hook 拆分为独立 gRPC 服务，以 wazero 执行 WASM Hook，结合 etcd 服务发现、OpenTelemetry、Prometheus/Grafana 与 mock benchmark 形成可演示、可观测的运行时闭环。
  \end{highlights}
\end{onecolentry}
```

边界说明：当前 ACP 已用于客户端到 Gateway 的流式通信、事件缓存与续传；**Agent-to-Agent Task/Result Collaboration Gateway 仅完成设计，尚未实现**，不应写入已交付能力。

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
- **W11：Agent Control Plane + Web Console 已完成代码实现**。新增持久 Agent Registry、Docker workspace volume、Control Plane HTTP/BFF、Redis-backed SSE 回放，以及浏览器端 Agent 创建、生命周期管理、多模型流式运行、run 历史和只读 workspace 浏览。
- **模型配置：使用 ModelScope `Qwen/Qwen3.5-35B-A3B`**。Worker 使用 `MODELSCOPE_ACCESS_TOKEN` 访问兼容协议端点；未配置的模型明确拒绝，不切换厂商或混用密钥。
- **当前验收入口：** [`STARTUP_GUIDE.md`](../STARTUP_GUIDE.md) 提供 Dashboard 完整启动与功能测试；[`CLI_RUNTIME_GUIDE.md`](./CLI_RUNTIME_GUIDE.md) 独立保留 W1–W10 Runtime 能力验证。
- **2026-07-26：前端端到端启动验证成功。** 在 Apple Silicon Docker Desktop 上完成依赖镜像拉取、8 个本地服务镜像构建、Postgres/Redis 健康检查及 Gateway、Worker、Control Plane、Web Console 启动；开发者可通过 `http://localhost:5173` 进入 Agent 管理界面进行完整前端验证。
- **2026-08-05：Dashboard 运行可见性与 Agent 管理增强完成。** 已交付实时依赖健康状态和活跃 Run 计数、工具调用时间线、运行中刷新页面后的 SSE 回放恢复、Agent 完整资源/工具/工作区概览，以及按服务端结果处理的批量真实删除。

## 技术栈

| 类别 | 技术 |
|---|---|
| 后端与协议 | Go 1.26.9、gRPC、Protocol Buffers、自研 ACP framed TCP、HTTP REST/SSE |
| Agent Runtime | Redis Streams、Redis Pub/Sub、mutable history、function calling、Supervisor、Pipeline DAG、context compaction |
| AI 与上下文 | ModelScope `Qwen/Qwen3.5-35B-A3B`、模型路由、Skill selector、RAG、pgvector、hybrid retrieval、reranker |
| 数据与基础设施 | Redis 7、PostgreSQL 16 + pgvector、etcd、Docker Engine API、Docker Compose、Nginx |
| 安全与扩展 | Docker L1 sandbox、resource limits、read-only rootfs、WASM/WASI、wazero Hook、tool allow-list |
| 可观测性 | OpenTelemetry、Prometheus、Grafana、OTLP Collector、structured logging |
| 前端 | React 18、Vite、TypeScript、Ant Design、React Router、TanStack Query、EventSource SSE、OpenAPI TypeScript types |
| 工程化 | Make、Buf、Go test、Vitest、Docker multi-stage build、GitHub |
