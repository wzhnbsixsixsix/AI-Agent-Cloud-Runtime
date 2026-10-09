# AgentForge 开发日志

本文档记录影响用户体验、运行时语义或部署方式的实际变更，作为代码提交之外的文字追溯。项目能力边界与路线图仍以 [`PROJECT_DESIGN.md`](../PROJECT_DESIGN.md) 为准。

## 2026-10-09

### Dashboard 视觉与响应式优化：运行信号台

- 实现提交：`b4d1d9f`。
- 记录归档：按项目记录约定，将本次设计决策、修改追溯、自检与验证结果合并到本日志，删除单独的设计记录文档，并更新样式文件注释引用；本次归档不改变页面行为。

#### 目标与设计决策

面向管理持久 Agent 的开发者，页面的首要任务是看清运行状态、创建 Agent 并进入执行工作区。以高品质网站的排版与交互完成度作为目标，不将外部奖项作为已获得的认证。

设计方向为「运行信号台」。保留项目已有的深蓝侧栏和青绿色识别，增加低饱和蓝灰仪表区域；避免将操作控制台改造成营销落地页。

- 色彩：Ink `#132b40`、Slate `#557386`、Canvas `#edf2f5`、Signal `#087f83`、Instrument `#e5eef1`、Surface `#ffffff`。
- 字体：主标题使用本地 Optima / Avenir Next 回退；正文 Avenir Next / 中文系统字体；数据使用系统等宽字体，不增加外部字体请求。
- 布局：桌面端为「工作目标 | 状态仪表」的双栏概览，其下为操作与列表；窄屏依次排列，表格在自身容器中横向滚动。
- 识别元素：信号条纹为装饰性图案，不表示吞吐量、时间序列或性能测量；下方三个数字来自实际 Agent 数据。

#### 修改追溯

| 文件 | 修改 | 原因 |
| --- | --- | --- |
| `web/src/main.tsx` | RegistrySignal 概览、真实注册/运行/失败数量、加载与异常占位 | 建立首屏层级，避免把未知状态表达为零 |
| `web/src/main.tsx` | 空注册表引导和创建入口 | 使首次使用有明确下一步 |
| `web/src/main.tsx` | 手机导航、当前页标识、跳过导航链接 | 修复原有小屏隐藏侧栏后无法导航的问题 |
| `web/src/main.tsx` | Agents、Runs、详情错误提示与重试 | 区分请求失败与真实空数据 |
| `web/src/main.tsx` | 两张表格独立横向滚动 | 保留窄屏全部数据与操作入口 |
| `web/src/main.tsx` | 主色调整为更深青绿色 | 提升主按钮白色文本可读性 |
| `web/src/styles.css` | 概览排版、间距、仪表、空状态、表格样式 | 形成一致的视觉节奏 |
| `web/src/styles.css` | 去除背景循环扫光、状态持续闪烁和操作卡片位移 | 避免装饰动效干扰阅读和操作 |
| `web/src/styles.css` | 焦点、响应式、长内容换行、抽屉控件换行 | 改善键盘与窄屏体验 |
| `web/src/styles.css` | 指南正文改用实际组件选择器，修复深色背景文字对比度 | 浏览器截图自检发现原有 p 选择器未命中 Paragraph |
| `web/index.html` | 标准文档结构、viewport、页面标题、描述、主题色 | 确保真实手机按设备宽度布局，并有明确标签页名称 |

#### 八维自检

1. 排版：主标题、页标题、数据、辅助文本建立不同角色；保留中文字体回退。
2. 留白：概览和操作区域通过间距与分隔线区分；表格去除多余外层内边距。
3. 层级：概览 → 操作 → 记录；创建为主要操作，删除保持危险语义。
4. 色彩：统一青绿色主色，错误保持红色语义；辅助文字使用较深蓝灰。
5. 动效：保留短暂入场和用户触发反馈；尊重系统减少动态效果偏好。
6. 微交互：链接方向反馈、按下反馈、焦点提示、重试、空状态行动入口。
7. 响应式：1150 / 900 / 720px 分级适配；小屏保留导航、表格和抽屉访问能力。
8. 原创性：围绕 Agent 工作区与注册状态设计仪表识别元素，不添加虚构性能数字或无关营销模块。

#### 验证记录

- `npm --prefix web run build`：通过 TypeScript 与 Vite 生产构建。
- `npm --prefix web test`：两个测试文件、三个现有测试全部通过。
- 浏览器自检：1280px 桌面首屏、390px 手机 Agents 与指南截图；实际打开创建抽屉，并通过导航访问指南和 Runs。
- 390px 抽屉边界为 0–390px；320px Agents 和 390px 指南的文档宽度等于视口宽度，无页面整体横向溢出。
- 截图检查后再次优化手机概览高度、文案间距和指南正文对比度。
- 后端当前未连接，验证了不可用状态；未声称完成真实 Agent 创建、任务运行或有数据列表的端到端验证。
- 初次构建因本地 esbuild 二进制缺失失败，按锁文件重新安装依赖后恢复；未修改依赖版本或锁文件。
- 既有依赖审计报告 14 项漏洞（6 moderate、6 high、2 critical）；构建提示主 JS 包约 1.19 MB（gzip 374 KB）。本次未执行可能破坏兼容性的依赖升级，包拆分与安全升级仍为后续工程工作。

本次不包含提交前已有的 `docs/FRONTEND_TEST_QUICKSTART.md` 与 `docs/TODO.md` 工作区修改。

### 开源基础能力调研与目标选型落盘（仅文档）

- 目标：优先复用可自托管的开源沙箱、工具循环和长期记忆方案，为每项选择保留文字依据。
- 修改：新增 `OPEN_SOURCE_RUNTIME_RESEARCH.md`，记录候选比较、官方来源、核验版本、采用/暂缓理由、源码接入事实及实施验收门槛；新增 ADR 006 汇总目标决策；文档索引新增入口，TODO 新增迁移专项和分阶段未完成清单。
- 修正：OpenSandbox 1.1.0 的 gVisor 与 egress 不兼容，快照也有后端限制；模型工具循环与 MCP 分别选型；AX latest 核验为 v0.3.1，其文档恢复语义不等于任意进程续跑；Mem0 OSS 不套用托管版基准。
- 边界：未修改代码、依赖、部署配置或当前功能状态；未执行组件部署及性能/效果评测。保留工作区已有 TODO 勾选和前端快速指南改动，不纳入本次文档提交。
- 验证：检查本次 Markdown 相对链接目标、文档 diff 与暂存范围；代码与部署待办均保持未完成，调研事实与推荐判断分开描述。提交记录提供本次五份文档的变更追溯。

## 2026-08-05

### Dashboard 的 Run 恢复与 Agent 批量管理

- 目标：使控制台在用户刷新页面或同时管理多个 Agent 时，仍能准确反映服务端的实际执行与删除结果。
- 修改：Agent 详情页会在加载时识别该 Agent 的活跃 Run，重新订阅 SSE 回放并恢复已收到的文本和工具时间线；Agent 列表补充完整的 Agent 概览，并支持批量删除。批量删除逐项调用服务端删除接口，仅在容器与 workspace 策略处理成功后从界面移除对应项；失败项保留并返回失败计数。
- 验证：相关提交为 `0dc1feb`（活跃 Run 恢复）、`86463db`（完整 Agent 概览）和 `96e0e64`（真实批量删除）；验收步骤已同步至 [`ACCEPTANCE_CHECKLIST.md`](./ACCEPTANCE_CHECKLIST.md)。

## 2026-08-04

### Run 工具调用时间线

- 目标：在模型调用工具的等待阶段持续向 Dashboard 提供可理解的进度，避免 Run 输出没有 token 时被误认为卡死。
- 修改：扩展 Run 的 protobuf、Redis Pub/Sub、Gateway gRPC 和 Control Plane SSE 契约，新增可回放的 `tool` 事件。每个调用先发送 `started`，再发送 `completed` 或 `failed`，包含工具名称、结果、错误和实际耗时；长结果会截断至 4 KiB，保护 SSE 回放与页面渲染。
- 修改：Agent 详情页的 Run 面板新增 Tool timeline，实时显示当前阶段、结果/错误、耗时和调用数；事件以 `callId` 合并，使同一次调用从运行中平滑更新为终态。
- 验证：新增 Agent Runner 断言，覆盖工具开始与成功完成事件；执行 Web 生产构建、Vitest 测试与 Control Plane 容器编译验证。

### Control Plane 实时健康状态与活跃 Run 计数

- 目标：让 Dashboard 顶部状态反映 Control Plane 的真实可用性，而不是固定显示 `Healthy`，并提供当前正在执行的 Run 数量。
- 修改：新增 `GET /api/v1/status`；健康检查会实际探测 PostgreSQL、Redis、Docker daemon 与 Gateway gRPC health，并从 PostgreSQL 统计 `status = 'running'` 的活跃 Run。任一依赖不可用时接口返回 `503` 和 `degraded` 状态，`/healthz` 同步采用该结果以便部署健康检查一致。
- 修改：Web Console 每 5 秒刷新状态，在顶部显示 `Healthy` 或 `Degraded`、当前活跃 Run 数；侧栏也不再固定声明已连接。状态标签的悬浮说明列出各依赖检查结果。
- 验证：Web 生产构建与 Vitest 测试通过；接口契约和对应 Dashboard 类型已写入 `api/controlplane.openapi.yaml` 与 `web/src/api.generated.ts`。

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
