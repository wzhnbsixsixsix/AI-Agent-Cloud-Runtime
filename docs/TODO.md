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
