# ADR 002：W10 只交付 Docker L1 Sandbox

## 状态

Accepted.

## 背景

早期设计里有一条 sandbox hardening 路线：Docker、gVisor、Firecracker、eBPF audit、CRIU restore。它们都合理，但如果全部在 W10 交付，会把项目重心从 Agent Runtime 变成 sandbox 研究项目，部署复杂度也会明显上升。

## 决策

W10 只交付 Docker L1 sandbox：

- 预热容器池
- sandbox command 默认无网络
- read-only rootfs
- tmpfs `/tmp`
- `cap_drop ALL`
- `no-new-privileges`
- memory、CPU、pids limit
- 单次 exec hard timeout

gVisor、Firecracker、eBPF、CRIU 作为未来安全加固方向保留在设计里，不写成已实现。

## 影响

- demo 可以在 Docker Desktop 上复现。
- 已足够展示 Agent tool execution 的隔离、资源限制和池化管理。
- 简历和面试材料不能声称 gVisor、Firecracker、eBPF 或 CRIU 已实现。
