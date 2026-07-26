# AgentForge 文档索引

本文档是 AgentForge 的统一阅读入口。每份文档只承担一种职责，避免在 README、设计文档和启动指南之间重复维护相同内容。

## 快速开始

| 文档 | 适合读者 | 内容 |
|---|---|---|
| [`STARTUP_GUIDE.md`](../STARTUP_GUIDE.md) | 首次运行项目的开发者 | Docker Compose、Dashboard 验收、日志与故障排查 |
| [`FRONTEND_TEST_QUICKSTART.md`](./FRONTEND_TEST_QUICKSTART.md) | 已熟悉项目的开发者 | 一页式启动、重建 Web、检查和停止命令 |

## 架构与设计

| 文档 | 职责 |
|---|---|
| [`PROJECT_DESIGN.md`](../PROJECT_DESIGN.md) | 项目定位、设计原则、模块边界、交付状态和路线图 |
| [`ARCHITECTURE.md`](./ARCHITECTURE.md) | 当前已实现的数据流、服务拆分、上下文和可观测架构 |
| [`CONTAINER_NETWORKING.md`](./CONTAINER_NETWORKING.md) | Compose 网络、服务寻址、端口、Docker socket 和隔离边界 |

## 运行时手册

| 文档 | 职责 |
|---|---|
| [`CLI_RUNTIME_GUIDE.md`](./CLI_RUNTIME_GUIDE.md) | W1–W10 CLI、Mock、Tool/Sandbox、Skill、RAG、Pipeline、Hook、Scheduler、Observability 和压测 |
| [`pkg/acp/spec.md`](../pkg/acp/spec.md) | ACP v1 帧格式与协议行为 |

## 验收与演示

| 文档 | 职责 |
|---|---|
| [`FINAL_DELIVERY.md`](./FINAL_DELIVERY.md) | 一页式交付范围和真实边界 |
| [`ACCEPTANCE_CHECKLIST.md`](./ACCEPTANCE_CHECKLIST.md) | 发布、录制和面试前的验收清单 |
| [`DEMO_SCRIPT.md`](./DEMO_SCRIPT.md) | 三分钟作品集或面试演示话术 |
| [`W9_BENCH_REPORT.md`](./W9_BENCH_REPORT.md) | 可复现压测记录 |

## 架构决策记录

| ADR | 决策 |
|---|---|
| [`001-acp-vs-grpc.md`](./adr/001-acp-vs-grpc.md) | ACP 与 gRPC 双入口 |
| [`002-sandbox-l1-scope.md`](./adr/002-sandbox-l1-scope.md) | Docker L1 sandbox 交付边界 |
| [`003-w8-service-split.md`](./adr/003-w8-service-split.md) | Skill/RAG/Hook 服务拆分 |
| [`004-w9-observability.md`](./adr/004-w9-observability.md) | OTel、Prometheus、Grafana 选型 |
| [`005-agent-control-plane-and-collaboration.md`](./adr/005-agent-control-plane-and-collaboration.md) | 已实现 Control Plane 与规划中的 ACP 协作网关 |

## 求职材料

| 文档 | 职责 |
|---|---|
| [`RESUME_TALK_TRACK.md`](./RESUME_TALK_TRACK.md) | 简历 bullet、STAR 话术和安全表述 |
| [`ENTERPRISE_OPS_DEMO.md`](./ENTERPRISE_OPS_DEMO.md) | 企业研发运维中台 fork 规划 |

## 推荐阅读路径

新开发者：

```text
README → STARTUP_GUIDE → Dashboard 使用指南 → ARCHITECTURE
```

后端或基础设施开发者：

```text
PROJECT_DESIGN → ARCHITECTURE → CONTAINER_NETWORKING → ADR → CLI_RUNTIME_GUIDE
```

作品集或面试：

```text
FINAL_DELIVERY → DEMO_SCRIPT → ACCEPTANCE_CHECKLIST → RESUME_TALK_TRACK
```
