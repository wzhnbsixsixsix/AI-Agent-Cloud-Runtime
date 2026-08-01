# AgentForge 开发日志

本文档记录影响用户体验、运行时语义或部署方式的实际变更，作为代码提交之外的文字追溯。项目能力边界与路线图仍以 [`PROJECT_DESIGN.md`](../PROJECT_DESIGN.md) 为准。

## 2026-08-01

### Workspace 支持返回上一级目录

- 问题：Agent Detail 的 Workspace 进入子目录后只能继续向下浏览，无法回到父目录。
- 修改：Workspace 增加“上一级”和“根目录”按钮，并显示当前路径；切换目录时清除旧文件预览。
- 交互规则：根目录下两个按钮禁用；进入子目录后启用；父目录通过当前相对路径计算，不允许跳出 Agent 的 `/workspace`。
- 验证：React TypeScript/Vite 生产构建通过；浏览器实测从 `/` 进入 `/tt`，可通过两个按钮分别返回 `/`，目录树恢复显示 `tt`。

### Agent 工具改用持久 Workspace

- 问题：同一 Run 内不同工具调用分别使用一次性 sandbox，导致 `mkdir tt` 后 `fs_list` 看不到 `tt`。
- 修改：Worker 根据 `agent_id` 定位 Control Plane 创建的持久 Agent 容器，文件和 shell 工具统一在该容器的 `/workspace` 中执行。
- 结果：同一 Run 的多个工具调用以及同一 Agent 的后续 Run 共享 named volume；临时 CLI Tool Sandbox 仍保留并与持久 Agent 执行目标并存。
- 验证：真实 Run 创建 `tt` 后，模型工具结果和 Workspace HTTP API 均返回 `tt`；Agent、Tool、Sandbox 和 Control Plane 相关测试通过。

### 修复工具调用导致 Worker 崩溃

- 问题：Worker 以非 root 用户访问 Docker Desktop socket 时权限不足；Docker driver 初始化错误又被包装成 typed-nil interface，首次工具调用触发 panic，前端持续等待。
- 修改：可信 Worker 控制组件获得 Docker socket 所需权限；初始化失败显式返回 nil；Tool Runner 增加 typed-nil 防御与回归测试。
- 验证：Worker 启动日志出现 `docker driver ready`，Docker sandbox pool 预热完成，真实 `fs_list` 调用成功且 Worker 不再重启。

## 记录约定

- 每次影响功能、接口、运行方式或用户体验的修改，都在本文件追加日期、问题、修改和验证结果。
- 纯格式调整或无行为变化的机械重排可以合并记录。
- 提交记录提供代码级追溯，本文件提供面向开发者和验收者的行为级追溯。
