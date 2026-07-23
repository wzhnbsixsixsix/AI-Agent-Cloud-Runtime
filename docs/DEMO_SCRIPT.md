# AgentForge 3 分钟 Demo 脚本

这份脚本适合录作品集视频，也适合面试 live walkthrough。

## 录制前准备

```bash
make final-check
LLM_PROVIDER=mock HOOK_ENABLED=true RAG_ENABLED=true make up
```

提前打开两个页面：

- Grafana: `http://localhost:3000`
- Prometheus: `http://localhost:9090`

## 0:00-0:25 — 项目定位

可以这样说：

> AgentForge 是一个 Go 实现的云原生 AI Agent Runtime。它重点不是做一个聊天应用，而是展示 Agent 执行隔离、动态上下文、多 Agent 编排、Hook 扩展和可观测性。

画面展示：

- README 顶部 W10 状态
- `docs/ARCHITECTURE.md` 架构图

## 0:25-0:55 — 基础 Run

```bash
./bin/agentctl run --prompt "用一句话介绍 AgentForge"
```

讲解：

> 这条命令走 `agentctl -> gateway -> Redis Stream -> worker -> LLM provider`，worker 再通过 Redis Pub/Sub 把 token 流式回传。外部入口同时支持 gRPC 和自研 ACP。

## 0:55-1:20 — Hook 安全拦截

```bash
./bin/agentctl hook run --addr localhost:8083 \
  --event PreToolUse \
  --file examples/hooks/pretool_bash.json
```

讲解：

> W8 把 Hook 拆成独立 gRPC 服务。Hook 可以在工具执行前拒绝危险命令，也可以在 LLM 前注入企业安全提示，或在工具执行后脱敏输出。

## 1:20-1:50 — RAG 和 Skill 上下文

```bash
./bin/agentctl rag ingest --path README.md --tenant default

./bin/agentctl rag query \
  --query "W9 可观测怎么工作" \
  --tenant default
```

讲解：

> RAG 检索到的内容会用 `<untrusted>` 包起来注入 context；Skill 则从 `SKILL.md` 的 name/description 中确定性选择。两者都不改变 `RunAgent` 公共接口。

## 1:50-2:15 — Multi-Agent Pipeline

```bash
./bin/agentctl pipeline run --file examples/pipeline/readme-review.yaml
```

讲解：

> W7 加了本地 Supervisor subagent 和轻量 pipeline DAG。child run 有独立 history，parent 只接收结构化结果。

## 2:15-2:40 — Grafana 可观测

切到 Grafana dashboard。

讲解：

> W9 接入 OpenTelemetry 和 Prometheus。dashboard 展示 run rate、p95 latency、token/s、tool/hook latency、worker capacity 等 runtime 指标。

## 2:40-3:00 — Mock Bench 收尾

```bash
make bench-run
```

讲解：

> 压测默认使用 mock LLM，这样数字反映 runtime 自身开销，而不是模型 API 的限流、价格或网络抖动。真实 GLM/OpenAI-compatible key 只用于 smoke test。

收尾一句：

> 这个项目的核心设计取舍是：每一周都增加 runtime 能力，但不破坏 `RunAgent`、ACP 和 Tool 的公共契约。
