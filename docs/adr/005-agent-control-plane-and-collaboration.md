# ADR 005：Agent Control Plane 与 ACP 协作网关

## 状态

Proposed.

## 背景

现有 AgentForge 提供稳定的 gateway/worker 执行链路、Docker L1 临时 sandbox，以及经 gRPC 拆分的 Skill、RAG 和 Hook 服务。ACP v1 用于 client ↔ gateway 的流式 Run/Event 与断线续传，尚不承担 Agent 之间的任务路由。

下一阶段需要提供 Web 前端，让用户创建多个拥有独立文件系统、配置和生命周期的 Agent；同时允许 Agent 将 RAG 等基础能力处理后的结论可靠地交给其他 Agent。

## 决策

新增以下控制与协作平面：

- **Agent Control Plane**：维护 AgentSpec（角色、模型、镜像、资源、权限、workspace 策略）及 Agent 生命周期。
- **Agent Manager**：为每个 Agent 管理一个持久容器和独立 workspace volume。
- **ACP Collaboration Gateway**：接受 Agent Task/Result/Progress/Failure，鉴权并路由到目标 Agent。
- **Redis-backed collaboration state**：持久化任务、状态和事件，支持离线投递、幂等、重试和回放。

基础能力服务继续使用 gRPC。典型路径是：Agent A 通过 gRPC 调 `RAGService`；A 将总结、置信度和 citations 封装为 ACP `knowledge_result`；Collaboration Gateway 将其投递给 Agent B。

Agent 容器不直接 P2P 通信，也不暴露 Docker socket。容器保持只读 rootfs，使用仅属于本 Agent 的持久 `/workspace` 卷，并设置 CPU、内存、PID、网络和 tool allow-list 限制。

## 影响

- ACP 从仅 Run/Event 的协议演进为兼容的协作任务协议；既有 ACP v1 和 `RunAgent` 不变。
- 现有 Docker L1 sandbox pool 继续服务临时 run/tool 执行；持久 Agent container 成为第二种 lifecycle，由新的 sandbox/agent manager 实现。
- 协作结果必须可追溯：包含 task/parent task、发送方/接收方、状态、trace、幂等键、置信度和 citations。大载荷通过 artifact ID 引用受控存储。
- 引入 Agent Registry、资源配额、容器/卷清理策略、租户鉴权和协作审计等新的运维责任。
