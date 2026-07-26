# ADR 005：Agent Control Plane 与 ACP 协作网关

## 状态

部分实现。

- Agent Control Plane、Web Dashboard、Agent Registry、持久 Agent 容器/workspace 和 Run SSE：已实现。
- ACP Agent-to-Agent Collaboration Gateway：Proposed，尚未实现。

## 背景

AgentForge 提供稳定的 gateway/worker 执行链路、Docker L1 临时 sandbox，以及经 gRPC 拆分的 Skill、RAG 和 Hook 服务。ACP v1 用于 client ↔ gateway 的流式 Run/Event 与断线续传，尚不承担 Agent 之间的任务路由。

Web 前端与 Control Plane 已让用户能够创建拥有独立文件系统、配置和生命周期的 Agent。下一阶段需要允许 Agent 将 RAG 等基础能力处理后的结论可靠地交给其他 Agent。

## 决策

控制与协作平面按两个阶段交付：

- **Agent Control Plane（已实现）**：维护 AgentSpec（角色、模型、镜像、资源、权限、workspace 策略）及 Agent 生命周期。
- **Agent Manager（已实现）**：为每个 Agent 管理一个持久容器和独立 workspace volume。
- **HTTP/SSE Web BFF（已实现）**：浏览器通过同源 API 管理 Agent，Control Plane 将 Gateway gRPC RunEvent 转换为可回放 SSE。
- **ACP Collaboration Gateway**：接受 Agent Task/Result/Progress/Failure，鉴权并路由到目标 Agent。
- **Redis-backed collaboration state**：持久化任务、状态和事件，支持离线投递、幂等、重试和回放。

当前基础能力调用由 gateway/worker 通过 gRPC 完成，持久 Agent 容器保持 `network=none`。未来典型协作路径是：Agent A 的可信 runtime 调用 `RAGService`；A 将总结、置信度和 citations 封装为 ACP `knowledge_result`；Collaboration Gateway 将其投递给 Agent B。

Agent 容器不直接 P2P 通信，也不暴露 Docker socket。容器保持只读 rootfs，使用仅属于本 Agent 的持久 `/workspace` 卷，并设置 CPU、内存、PID、网络和 tool allow-list 限制。

## 影响

- ACP 从仅 Run/Event 的协议演进为兼容的协作任务协议；既有 ACP v1 和 `RunAgent` 不变。
- 现有 Docker L1 sandbox pool 继续服务临时 run/tool 执行；持久 Agent container 成为第二种 lifecycle，由新的 sandbox/agent manager 实现。
- 协作结果必须可追溯：包含 task/parent task、发送方/接收方、状态、trace、幂等键、置信度和 citations。大载荷通过 artifact ID 引用受控存储。
- 引入 Agent Registry、资源配额、容器/卷清理策略、租户鉴权和协作审计等新的运维责任。

当前实现与未来协作的边界见 [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md)；容器网络见 [`docs/CONTAINER_NETWORKING.md`](../CONTAINER_NETWORKING.md)。
