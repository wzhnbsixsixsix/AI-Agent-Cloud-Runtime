# ADR 006：优先复用开源 Agent 基础能力

- 日期：2026-10-09
- 状态：目标选型与兼容迁移方向；尚未实现，生产切换以集成验证为门槛
- 证据与完整比较：[开源 Runtime 调研](../OPEN_SOURCE_RUNTIME_RESEARCH.md)

## 背景

AgentForge 已有 Go 控制面、队列、Docker 执行、手写工具循环、Redis History 和演示 RAG。后续优先采用可自托管的开源组件，把开发投入集中在平台集成、任务管理、权限与可观测性。用户已选择兼容迁移、Mem0 长期记忆与 AX 暂不接入。

## 决策

1. OpenSandbox 作为沙箱管理首选，覆盖临时执行和 Dashboard 持久 Agent 的真实路径。SDK 负责命令/文件/生命周期，AgentForge 负责 Agent 与 workspace 归属。
2. 无直接外网执行验证 gVisor；受控联网验证 Kata QEMU + egress。OpenSandbox 1.1.0 的 gVisor/egress 组合不兼容且不支持该快照路径，禁止照搬上一版全功能组合。安全运行时不可用不自动降级。
3. Eino 作为 Go Agent 循环首选，官方 MCP Go SDK 作为外部工具互操作首选。内部 sandbox 工具直接调用 SDK；仅实现类型、策略和事件适配，不重写上游协议。
4. Mem0 OSS + pgvector 负责长期记忆；History 负责运行/会话事件，RAG 负责显式导入知识。记忆失败可在可观测前提下降级，身份过滤不可绕过。保留旧 History，不自动批量导入。
5. Google AX 不集成；ADK Go、E2B、ReMe、Graphiti 保留为对应需求变化时的备选。OpenViking 在未来统一上下文时重新评估，不在本期同时替换 Skill/RAG/Memory。

## 取舍理由

- OpenSandbox 提供与现有 Docker 起点匹配的服务 API 和 Go SDK，降低自建 sandbox 管理成本；Kata 的虚拟化要求是联网隔离方案的部署代价。
- Eino 的 Go 组件及中间件便于映射当前模型、Hook 和 SSE。ADK Go 也支持 Chat Completions，但会引入不同的会话/事件模型；不能据品牌直接判断优劣。
- MCP 不负责模型循环，不强制内部工具服务化。Eino 现有 MCP 扩展并非官方 SDK，选择官方 SDK 时只补最小桥接。
- Mem0 独立记忆职责和 pgvector 后端适合渐进迁移；它并非“大公司自研”，选择依据是开源与适配性。其托管版基准不作为 OSS 的效果承诺。
- AX 与控制面职责重叠且仍在演进；ReMe 偏文件与本地工作空间；Graphiti 引入图数据库；OpenViking 同时扩展到知识与 skills，且当前为 AGPLv3，均不是当前最小迁移范围。

## 影响与实施约束

升级 Go module、构建镜像和 CI 的工具链后才接入新 SDK；固定兼容组件版本。保留外部 API，逐路径验证后切换。第一阶段承认现有单用户边界，不能将传入 user_id 当作完成身份认证。workspace 迁移保留原数据，非幂等工具不自动重放。

需要通过沙箱生命周期、真实命令取消、网络策略、MCP 工具契约、模型兼容、跨 Run 记忆与隔离测试。详细阶段和验收见调研报告第 7 节，执行清单见 [TODO](../TODO.md)。本 ADR 不改变当前已交付架构说明。
