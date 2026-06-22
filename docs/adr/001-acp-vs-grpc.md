# ADR 001：为什么保留 ACP 与 gRPC 双入口

## 状态

Accepted.

## 背景

gRPC 已经能很好地解决流式 RPC、生态工具和跨语言调用问题。但 AgentForge 也希望展示协议层设计能力，例如帧格式、断线续传、事件缓存、小消息开销和未来自定义背压。

## 决策

保留两个公开入口：

- **gRPC**：作为工程兼容性和生态友好的主入口。
- **ACP**：作为自研双向流协议实验面，用于演示 framed stream、resume 和协议控制。

## 影响

- 可以在同一 gateway/worker 路径上公平比较 ACP 和 gRPC。
- 需要维护 `internal/acp` 和 `pkg/acp` 的额外代码。
- ACP 的 Window Update、compression、零拷贝热路径仍是未来增强，不作为 W10 已交付能力宣传。
