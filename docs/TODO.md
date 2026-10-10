# AgentForge TODO

本文档记录尚未交付的功能，并区分前端缺口、需要 Control Plane/BFF 支持的能力，以及需要新增后端设计的长期规划。完成状态以代码、测试和 [`DEVELOPMENT_LOG.md`](./DEVELOPMENT_LOG.md) 的验证记录为准。

## 当前前端基线

Dashboard 已实现 Agent 创建、启动、停止、删除、模型与资源配置、单 Agent Run、SSE 流式文本、全局 Run 列表，以及只读 Workspace 目录和文本文件浏览。

## P0：运行可靠性与可见性

- [ ] 页面刷新后自动识别当前 Agent 的活跃 Run，并恢复输出面板。
- [ ] SSE 异常断开后自动重连，并使用事件 ID 回放未显示的事件。
- [x] 增加 Tool 调用时间线，展示工具名称、执行阶段、结果、错误和耗时，避免工具执行期间看起来像卡死（2026-08-04）。
- [ ] 增加 Run 详情页，展示完整输出、状态时间线、错误、Trace ID、Token 数和执行时间。
- [ ] 在 Runs 页面支持进入历史 Run，并回放仍在保留期内的 SSE 事件。
- [x] 将顶部 Control Plane `Healthy` 改为真实健康检查结果，并显示当前活跃 Run 数量（2026-08-04）。
- [ ] 完善 Agent provisioning 体验：显示创建进度、失败原因和重试入口，不能把 `failed` Agent 提示为创建成功。
- [ ] 在 Agent 详情中展示 `lastError`，为生命周期操作提供明确的 loading、成功和失败反馈。

> Tool 时间线需要先扩展 Gateway → Control Plane → SSE 事件契约；当前 UI 只能收到 `state`、`token`、`done` 和 `error`。

## P1：已有 Runtime 能力的 Dashboard 入口

以下能力已存在于 gRPC、服务端或 CLI，但需要先增加 Control Plane REST/BFF 接口，再接入前端：

- [ ] RAG 文档管理：上传、入库、查询、结果预览和删除。
- [ ] Skill 管理：列表、详情、启用状态，以及 Run 的实际匹配结果。
- [ ] Multi-Agent Supervisor 与 Pipeline DAG 创建、运行和步骤状态展示。
- [ ] WASM Hook 列表、启停、策略和执行记录。
- [ ] Scheduler leader、worker 节点、服务发现和任务分配状态。
- [ ] Prometheus/Grafana 可观测入口，以及按 Run/Trace 跳转。
- [ ] Tool 注册表与受控的单工具调试页面。
- [ ] ACP/gRPC 调试、断线恢复和性能对比页面。

## P1：Agent 与 Run 管理增强

- [ ] 编辑 Agent 的名称、角色、System Prompt、模型、资源额度、工具权限和 Workspace 策略。
- [ ] 克隆 Agent 配置并创建新的独立容器和 Workspace。
- [ ] Agents 列表增加搜索、状态筛选、排序、分页和批量生命周期操作。
- [ ] Agent 概览完整展示 PID 限制、工具 allow-list、Workspace 策略、Container ID 和时间戳。
- [ ] Runs 页面增加 Agent、模型、状态和时间范围筛选。
- [ ] 支持失败 Run 一键重试。
- [ ] 支持取消活跃 Run；需要后端提供可安全终止的 Run API 和状态语义。
- [ ] 支持复制或导出 Run 的 Prompt、输出和诊断信息。

## P2：Workspace 与运维体验

- [ ] Workspace 文件下载及图片、PDF 等受控预览。
- [ ] 展示文件大小、类型和修改时间等元数据。
- [ ] 支持 Agent 停止时以只读方式浏览其 named volume。
- [ ] 增加容器日志、CPU、内存和运行时间查看入口。
- [ ] 提供 Workspace 上传、创建、编辑、重命名和删除能力；必须先补充路径校验、大小限制、并发写入和审计策略。
- [ ] 展示 Workspace 文件变化或 Git 状态。

## P3：平台与协作能力

以下能力尚未在当前后端交付，不能只通过增加前端页面完成：

- [ ] 实现 Agent-to-Agent ACP Task/Result/Progress/Failure Collaboration Gateway。
- [ ] 增加多 Agent 拓扑画布、任务编排、协作状态和结果传递 UI。
- [ ] 增加协作任务的鉴权、幂等、重试、回放、审批和审计。
- [ ] 实现登录、用户管理、多租户和 RBAC。
- [ ] 增加 API Key/Secret 的安全管理与脱敏页面。
- [ ] 补充 gVisor/Firecracker、eBPF 审计等更强隔离能力及其管理入口。

## 开源基础能力迁移专项（2026-10-09，后端与部署工程）

选型依据、各方案采用/暂缓理由、官方来源及待验证限制见 [开源 Runtime 调研](./OPEN_SOURCE_RUNTIME_RESEARCH.md) 与 [ADR 006](./adr/006-open-source-runtime-components.md)。P0 已开展独立验证，未切换业务默认路径；真实隔离环境未验收，不能宣称迁移已完成。

- [x] 落盘沙箱、Tool use/框架、Memory 的调研比较、选型理由和版本证据；记录 gVisor/egress 不兼容、AX 恢复语义、Mem0 OSS/托管版边界等修正。
- [x] 按用户要求移除 GLM 模型配置，不再等待 GLM 429 恢复（2026-10-10）：Worker/Compose/本地环境、默认模型、前端/API 与真实验证入口改为 ModelScope Qwen；旧 Provider 和未知模型明确拒绝，保留历史 Agent/workspace。相关单测与前端构建通过；真实 Qwen 结果单独计入下项，不以单测代替。
- [ ] P0：锁定上游 tag/commit、SDK 与镜像组合；验证 Go 工具链升级以及 Qwen 模型兼容性（用户已撤销 GLM 要求）。进展（2026-10-10）：OpenSandbox 标签/构建提交、官方 BOM 镜像摘要、SDK/依赖校验和已有锁定；新增固定 Docker 官方 repo-info 提交的 Go/Python workload index 摘要，Go 构建使用 tag@digest。Go 1.26.9 回归、P0 离线检查通过；本轮真实 Qwen 原 Provider 返回 HTTP 403，Eino 也失败，不能替代为历史成功。待恢复 Qwen 访问后跑通统一入口，并完成 registry manifest、镜像拉取与供应链验证，未整体完成。
- [ ] P0：完成 OpenSandbox 最小集成验证，分别测试无网 gVisor 与联网 Kata QEMU/egress；运行时不可用时禁止静默降级。进展（2026-10-10）：隔离验证入口、两套配置、运行时缺失拒绝与 SDK 不自动重试单测通过；workload 已锁定摘要并增加 bash/Python 检查。真实入口本轮仍在 Docker 不可用时明确失败。用户确认没有 Linux 主机；仍需专用 Linux/gVisor/Kata QEMU 环境执行启动拒绝、生命周期/文件/命令和网络探针，不能用本地单测代替。
- [ ] P1：接入 OpenSandbox 生命周期、命令和文件 API；同时覆盖临时工具路径与 Dashboard 持久 Agent，不仅替换预热池。
- [ ] P1：设计并验证持久 workspace 的 stop/start、跨 Run 复用、浏览、删除与显式迁移/回滚；保留原 volume 数据。
- [ ] P1：以 Eino 接管模型—工具循环，映射现有 Hook、工具权限、调用 ID、错误与 SSE 事件，保留外部契约。
- [ ] P1：使用官方 MCP Go SDK 接入受控外部工具，完成最小 Eino 类型桥接；内部 sandbox 工具直接复用 OpenSandbox SDK。
- [ ] P1：统一 Gateway 工具发现与 Worker 执行目录，验证 allow-list、取消、输出限制及非幂等工具不自动重放。
- [ ] P1：部署自托管 Mem0 OSS + pgvector，锁定 Server 源码/依赖，配置真实 embedding 并验证模型兼容。
- [ ] P1：明确 session/run/agent/user 范围，先实现单用户按 Agent 隔离；后端生成记忆过滤条件并校验按 ID 操作的归属。
- [ ] P1：接入运行前召回、运行后持久写入任务、去重/重试、来源记录、故障可见与降级；提供查询、更正、删除和显式旧历史导入。
- [ ] P2：执行同配置记忆效果评测及沙箱冷热启动/并发测试，记录环境、版本、成本与失败率，不引用厂商数字作为本项目实测。
- [ ] P2：完成 CLI/Dashboard/gRPC/ACP/SSE/模型/workspace 回归后逐项切换默认路径，同步架构、部署和验收文档。

专项顺序为版本与契约验证 → 沙箱及持久 workspace → Eino/MCP → Mem0 → 验收切换。Google AX 暂不集成；OpenViking、ReMe、Graphiti、E2B、ADK Go 的重新评估条件见调研报告，不默认同时实现多个 backend。

## 推荐实施顺序

1. Run 刷新恢复、SSE 重连、Tool 调用时间线。
2. Run 详情、失败诊断、重试与取消。
3. Agent 编辑、真实健康状态和资源监控。
4. RAG 文档管理。
5. Pipeline/Multi-Agent 与 ACP Collaboration。

## 维护规则

- 新增待办时写清楚是否只需前端、需要 BFF，或需要完整后端能力。
- 完成待办后勾选条目，并在 [`DEVELOPMENT_LOG.md`](./DEVELOPMENT_LOG.md) 记录实现、验证结果和提交日期。
- 规划能力不得写成已交付功能；实际边界以 [`ARCHITECTURE.md`](./ARCHITECTURE.md) 和 [`PROJECT_DESIGN.md`](../PROJECT_DESIGN.md) 为准。
