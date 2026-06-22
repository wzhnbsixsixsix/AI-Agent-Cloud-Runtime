# ADR 003：为什么拆出 Skill / RAG / Hook 服务

## 状态

Accepted.

## 背景

W5-W7 期间，Skill、RAG、Hook 和编排逻辑逐步进入 worker 路径。早期都放在 worker 内部能减少联调复杂度，但长期看会隐藏服务边界，也不利于独立扩缩容、故障隔离和服务发现演示。

## 决策

W8 将三类能力拆成独立 gRPC 服务：

- `skilld`
- `ragd`
- `hookd`

gateway 保留原有 `AgentService.IngestRAG` / `QueryRAG` 对外入口，内部代理到 `ragd`。worker 通过配置地址访问 Skill/RAG/Hook 服务。

## 影响

- `RunAgent`、ACP 和 tool contract 保持兼容。
- docker compose 更重，但拓扑更接近生产环境。
- 单测仍可注入 in-memory fake client。
- 可以在不重写 Redis Stream 主消费链路的前提下，用 etcd 演示服务发现和 scheduler election。
