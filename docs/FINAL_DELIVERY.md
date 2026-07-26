# AgentForge 最终交付说明

AgentForge 是一个云原生 AI Agent Runtime。它不是单个聊天机器人，而是一套用于提交、执行、隔离、编排、扩展和观测 Agent Run 的运行时基座。

本仓库是 **runtime mainline**，只保留通用基础设施能力；企业 Lark 中台等业务 demo 应该放到 fork 或分支，通过 CLI/gRPC 调用本 runtime。

## 已交付能力

- **Web Control Plane**：React Dashboard、Agent Registry、持久 Agent 容器与 workspace、生命周期管理、Run/SSE 回放和只读文件浏览。
- **执行链路**：`agentctl -> gateway -> Redis Stream -> worker -> LLM`，支持流式 token 和 history 持久化。
- **ACP 协议**：在 gRPC 旁边提供自研 TCP framed stream，并支持 resume。
- **Sandbox Tool**：Docker L1 sandbox 预热池，内置 bash、文件、HTTP 工具和 function-calling loop。
- **上下文工程**：mutable history fold、Skill selector、pgvector RAG、`<untrusted>` context 注入。
- **多 Agent 编排**：本地 Supervisor subagent、pipeline DAG、自动上下文压缩。
- **WASM Hook 与服务拆分**：`skilld`、`ragd`、`hookd`、wazero WASI hook、etcd discovery、scheduler leader/pick。
- **可观测与压测**：OpenTelemetry、Prometheus、Grafana dashboard、mock RunAgent benchmark。

## 快速命令

新用户默认从 Dashboard 开始：

```bash
cp .env.example .env
DOCKER_DEFAULT_PLATFORM=linux/arm64 \
  docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
```

浏览器打开 `http://localhost:5173`。完整步骤见 [`STARTUP_GUIDE.md`](../STARTUP_GUIDE.md)。

CLI Runtime 验证：

```bash
make proto
make build
make up
./bin/agentctl run --prompt "用一句话介绍 AgentForge"
```

可复现压测默认使用 mock LLM：

```bash
LLM_PROVIDER=mock HOOK_ENABLED=true RAG_ENABLED=false make up
make bench-run
```

可观测入口：

- Grafana: `http://localhost:3000`，默认 `admin/admin`
- Prometheus: `http://localhost:9090`
- Dashboard: `AgentForge / AgentForge W9 Runtime`

## 演示入口

- 文档总索引：[`docs/README.md`](./README.md)
- Dashboard 启动：[`STARTUP_GUIDE.md`](../STARTUP_GUIDE.md)
- CLI Runtime 手册：[`docs/CLI_RUNTIME_GUIDE.md`](./CLI_RUNTIME_GUIDE.md)
- 当前架构：[`docs/ARCHITECTURE.md`](./ARCHITECTURE.md)
- 容器通信：[`docs/CONTAINER_NETWORKING.md`](./CONTAINER_NETWORKING.md)
- 3 分钟 Demo：[`docs/DEMO_SCRIPT.md`](./DEMO_SCRIPT.md)
- 最终验收清单：[`docs/ACCEPTANCE_CHECKLIST.md`](./ACCEPTANCE_CHECKLIST.md)
- 简历与面试话术：[`docs/RESUME_TALK_TRACK.md`](./RESUME_TALK_TRACK.md)
- W9 压测报告模板：[`docs/W9_BENCH_REPORT.md`](./W9_BENCH_REPORT.md)

## 真实边界

已实现并可 demo：

- Web Console、Control Plane、Agent Registry、持久容器/workspace 和 Run SSE
- Docker L1 sandbox
- ACP/gRPC 双入口
- Skill、RAG、Multi-Agent、context compaction
- wazero Hook、服务拆分、etcd-backed election
- Prometheus/Grafana dashboard、mock benchmark

已设计但未在 mainline 实现：

- Agent-to-Agent ACP Task/Result Collaboration Gateway
- gVisor、Firecracker、eBPF syscall audit、CRIU checkpoint restore
- Loki、Tempo
- worker-specific queue sharding
- Lark 企业业务集成

## 面试定位

可以这样介绍：

> AgentForge 是一个 Go 实现的 AI Agent Runtime，重点在执行隔离、动态上下文、多 Agent 编排和可观测性。它保持 `RunAgent` / ACP / Tool 公共协议稳定，同时在 worker 和服务层逐步加入 Skill、RAG、Hook、Scheduler 和 Observability 能力。
