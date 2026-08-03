# AgentForge 开发日志

本文档记录影响用户体验、运行时语义或部署方式的实际变更，作为代码提交之外的文字追溯。项目能力边界与路线图仍以 [`PROJECT_DESIGN.md`](../PROJECT_DESIGN.md) 为准。

## 2026-08-03

### Dashboard 前端视觉系统升级：运行控制室

- 目标：将通用浅色管理界面调整为与 AgentForge 的 Agent 编排、运行状态和控制平面语义一致的运行控制台，同时保持现有 API、SSE 和操作流程不变。
- 修改：在 `web/src/main.tsx` 中统一 Ant Design 的颜色、文字、圆角与菜单主题，并为 Agents 和 Runs 的标题、数据表提供面向“注册表 / 执行台账”的信息层级；侧栏新增运行时身份与 Control Plane 连接状态。
- 视觉：在 `web/src/styles.css` 建立深石墨、金属蓝灰、电光青和状态绿的 token 系统；使用低对比信号网格作为侧栏的标志性元素，重构导航、顶部环境栏、表格、卡片、运行输出与使用指南 Hero 的层次和反馈。
- 可访问性：保留原有 `prefers-reduced-motion` 降级规则；不改动路由、REST、SSE、数据模型或 Agent 生命周期操作。
- 验证：TypeScript/Vite 生产构建与 Vitest 测试通过。

## 2026-08-01

### 建立 Dashboard 与平台功能 TODO 基线

- 目标：集中记录当前 Dashboard 尚未覆盖的能力，并明确“前端可直接补齐”“需要 Control Plane/BFF”“需要新增后端设计”三类边界。
- 修改：新增 [`TODO.md`](./TODO.md)，按 P0–P3 记录 Run 恢复与 Tool 可见性、Runtime 管理入口、Agent/Workspace 增强及 ACP 协作与权限体系等待办，并给出推荐实施顺序。
- 文档治理：将 TODO 加入 [`docs/README.md`](./README.md) 的开发记录索引；后续完成事项时同步勾选清单并在本日志记录验证结果。
- 验证：检查 Markdown 链接指向现有文档，确认待办与当前 REST/SSE 边界及已实现功能描述一致。

### Dashboard 增加克制企业级动效

- 目标：在不改变功能流程和数据语义的前提下，为开发者控制台补充清晰、低干扰的操作与状态反馈。
- 修改：新增 150–300ms 统一 motion tokens；Agents、Runs、Agent Detail 和使用指南增加路由淡入与轻微上移；卡片、表格、按钮、状态 Tag、Run 输出和 Workspace 目录切换增加局部反馈。
- 运行反馈：`running`、`provisioning` 和 Control Plane 健康状态使用低强度呼吸效果；Run 活跃时输出面板显示柔和高亮和流式光标，终态后随 React 状态自动停止。
- 指南与目录：使用指南 Hero 使用低频低透明度流光，能力卡片错峰入场；Workspace 路径、目录树和文件预览在切换时短暂淡入。
- 可访问性：通过 `prefers-reduced-motion: reduce` 关闭循环、位移和错峰动画；没有引入新的 npm 动画依赖，也没有修改 REST、SSE、OpenAPI 或 gRPC 接口。
- 验证：Vitest 通过，TypeScript/Vite 生产构建通过；浏览器确认四个路由、创建 Drawer、状态标签、Run 面板样式规则及 `/ → /tt` Workspace 切换动效生效，未发现布局抖动或交互遮挡。

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
