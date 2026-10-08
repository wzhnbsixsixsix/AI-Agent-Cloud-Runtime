# 开源 Agent Runtime 基础能力选型调研

核验日期：2026-10-09。状态：文档与源码调研完成，尚未进行组件部署、兼容性实测或性能评测。本文件不表示相关能力已经接入 AgentForge。

## 1. 目标、方法与证据边界

目标是优先复用持续维护、可自托管的开源方案，减少自行实现沙箱、模型工具循环与长期记忆。大公司背景是维护能力的参考，不代替开源边界、版本兼容性与项目适配性判断。保留 Go 主服务、现有外部 API 和本地开发路径，采用兼容迁移。

本轮覆盖约二十个项目，对主要候选查阅官方仓库、发布记录、固定版本文档、依赖声明和关键源码。MemOS 等外围候选仅完成初筛；不声称覆盖所有方案或完成全面安全审计。表中的推荐是工程判断，厂商性能和记忆准确率不视为本项目实测结果。

证据优先级：固定版本源码/文档 > 官方发布记录 > 官方主分支文档 > 项目宣传。主分支能力与稳定发行版能力分别标注；实施前重新核对版本并固定 tag、commit 和镜像 digest，不使用浮动 latest。

## 2. 推荐组合与职责

| 层次 | 推荐 | 选择理由 | 边界 |
|---|---|---|---|
| 沙箱管理 | OpenSandbox | 统一生命周期、命令、文件与网络 API；官方 Go SDK；Docker 到 Kubernetes 的迁移路径 | 不自行实现容器编排；必须核验具体 backend 能力 |
| 无直接外网的隔离执行 | gVisor | 可接入容器生态，适合现有 shell/文件工作负载 | 需要实测系统调用兼容性与网络限制 |
| 受控联网隔离执行 | Kata QEMU + OpenSandbox egress | 完整 guest kernel 能承载当前 egress 所需 NAT 能力 | 要求 Linux 虚拟化基础设施；不得假定本机 Docker Desktop 可直接验收 |
| 模型—工具循环 | CloudWeGo Eino | Go 原生，具备工具循环、流式事件、中间件及编排能力 | 0.x 版本需固定；框架 checkpoint 不等于进程或工具副作用自动恢复 |
| 外部工具互操作 | 官方 MCP Go SDK | 复用协议、传输与工具发现实现 | MCP 不替代模型循环；不强制内部 SDK 调用绕行 MCP |
| 长期记忆 | Mem0 OSS + pgvector | 独立服务、开源、与已有 PostgreSQL 基础设施匹配 | 不自动提供业务租户隔离；不等同托管版效果 |
| 后续统一上下文 | OpenViking，暂不接入 | 可统一知识、记忆、skills 和分层读取 | 改造范围大于本期；当前许可证为 AGPLv3 |
| 上层集群编排 | Google AX，暂不接入 | 有参考价值的 Task/Workspace/Model 设计 | 与现有控制面重叠，仍有架构变动风险 |

AgentForge 继续负责用户入口、Agent 配置、任务队列、身份与权限边界、事件转换、审计和可观测性。允许实现薄适配器；避免重新实现上游已经承担的协议、隔离内核、记忆提取与工具循环。

## 3. 沙箱与运行基础设施对比

| 候选及官方来源 | 核验内容 | 采用或暂缓理由 |
|---|---|---|
| [OpenSandbox](https://github.com/opensandbox-group/OpenSandbox) | Apache-2.0；官方仓库由原 Alibaba 地址重定向；Go SDK、Docker/Kubernetes、Execd、Egress | 首选：与当前本地 Docker 开发及未来集群部署匹配 |
| [Kubernetes Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox) | Sandbox、Template、WarmPool 等 CRD 与 Go SDK | Kubernetes 阶段可通过 OpenSandbox provider 使用，不重复建设两套上层生命周期控制 |
| [E2B Runtime](https://github.com/e2b-dev/runtime) | Apache-2.0、Go、Firecracker、暂停/恢复；Embed 要求 Linux/KVM，官方定位评估部署 | 强备选；若 microVM 快照、高密度执行成为核心目标再比较，当前引入整套基础设施成本较高 |
| [Google AX](https://github.com/google/ax) | Task/Workspace/Model；依赖 Agent Substrate；官方警告可能有重大变更 | 依用户选择仅调研，避免与已有控制面和调度重叠 |
| [gVisor](https://gvisor.dev/docs/) | 用户态内核与 runsc；存在 syscall 兼容性和额外开销 | 无网执行候选，不能把它描述为零成本或完整 Linux 内核 |
| [Kata Containers](https://github.com/kata-containers/kata-containers) | Apache-2.0；VM 隔离、guest kernel、硬件虚拟化要求 | 受控联网优先验证，兼容性收益需与资源成本一起测量 |
| [Firecracker](https://github.com/firecracker-microvm/firecracker) | microVM 基础层 | 通过现有平台使用；直接接入会重新承担镜像、网络、存储与生命周期管理 |
| [Hyperlight](https://github.com/hyperlight-dev/hyperlight) | pre-1.0；Rust 嵌入式 VMM，guest 无通用 OS，需专门构建 | 不适合直接承接 bash、Git 和普通 Linux 工具链 |
| [Daytona](https://github.com/daytonaio/daytona) | 当前公开仓库声明核心开发自 2026-06 转为私有且仓库不再维护 | 不作为持续维护的开源默认方案；历史开源版本不等同当前产品 |

### OpenSandbox 关键限制

已核验统一稳定发布 [release-1.1.0](https://github.com/opensandbox-group/OpenSandbox/releases/tag/release-1.1.0)。[该版本 Go SDK](https://github.com/opensandbox-group/OpenSandbox/blob/release-1.1.0/sdks/sandbox/go/README.md) 覆盖生命周期、命令流、文件与出口策略，module 仍为 `github.com/alibaba/OpenSandbox/sdks/sandbox/go`。统一发布是组件兼容基线，不保证所有 backend 的每项能力完全一致。

[1.1.0 安全运行时文档](https://github.com/opensandbox-group/OpenSandbox/blob/release-1.1.0/docs/guides/secure-container.md) 明确说明：egress sidecar 用 iptables NAT REDIRECT 拦截 DNS，gVisor 的相关 NAT 能力不满足要求；`gvisor + network_policy` 组合会被拒绝。该发布也拒绝不支持的 gVisor 快照。不能直接承诺“gVisor + egress + Credential Vault + 快照”全部可用。

建议部署分层：本地 OpenSandbox/Docker 验证 API；生产无网路径验证 gVisor；受控联网路径验证 Kata QEMU/egress。已有 Cilium 的集群可研究 CNI FQDN 策略，但它不自动等价于 OpenSandbox 凭据代理。安全运行时不可用时应明确失败，不自动降级到普通 Docker。具体网络策略与凭据功能组合仍须部署测试。

### AX 的版本与恢复语义

[latest 核验结果为 v0.3.1](https://github.com/google/ax/releases/tag/v0.3.1)，上一轮“当前只有 v0.1”的说法已过时。[当前 runner 文档](https://github.com/google/ax/blob/main/docs/runner.md) 描述恢复 workspace 后启动新进程树，不能理解为任意执行中程序原地续跑。Task 生命周期与 AgentForge 的 Run 终态、执行退出码之间仍需语义映射。

## 4. Tool use 与 Agent 框架

模型决定调用工具、执行工具并回填结果的循环属于 Agent 框架。工具发现与跨服务调用属于 MCP。两层应分别复用，单独接入 MCP 并不会消除现有手写循环。

| 候选及官方来源 | 能力与适配代价 | 判断 |
|---|---|---|
| [Eino](https://github.com/cloudwego/eino) | Apache-2.0；Go 原生组件、ChatModelAgent、流式处理、图编排、中断恢复 | 优先用它替换内部循环，保留既有外部事件契约 |
| [Google ADK Go](https://github.com/google/adk-go) | Apache-2.0；Agent/session/tool/memory 与多 Agent 体系 | 主要备选；不是只能接 Gemini，但依赖及会话模型迁移面更大 |
| [Microsoft Agent Framework](https://github.com/microsoft/agent-framework) | Python/.NET Agent、tool、workflow | 能力相关；当前 Go 项目引入额外运行时成本较高 |
| [AWS Strands](https://github.com/strands-agents/sdk-python) | Python/TypeScript Agent loop、工具、MCP、session | 跨语言改造成本高于 Go 原生候选 |
| [LangGraph](https://github.com/langchain-ai/langgraph) | Python/JS 状态图、持久执行、checkpoint、人工介入 | 复杂持久工作流强备选；本期避免引入第二套执行架构 |
| [官方 MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) | 协议客户端/服务端、传输、工具发现及 OAuth 相关能力 | 外部工具互操作首选，授权边界仍由应用保证 |

核验 [Eino v0.9.21](https://github.com/cloudwego/eino/releases/tag/v0.9.21) 及 [ChatModelAgent 源码](https://github.com/cloudwego/eino/blob/v0.9.21/adk/chatmodel.go)，存在模型输入定制、工具中间件与事件接口，可承接 Hook、动态上下文与 SSE。适配仍需验证 tool call ID、流式参数、取消和失败语义，不能认为替换依赖即可完成。

[ADK Go v2.5.0](https://github.com/google/adk-go/releases/tag/v2.5.0) 已增加 Chat Completions 支持；[固定版本 go.mod](https://github.com/google/adk-go/blob/v2.5.0/go.mod) 要求 Go 1.26.6。不能沿用旧信息将它认定为不支持 OpenAI-compatible 路径。

[MCP 官方兼容表](https://github.com/modelcontextprotocol/go-sdk#version-compatibility) 表示 v1.7.0 起支持 `2026-07-28`；核验到 [v1.8.0](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0)，其 [go.mod](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/go.mod) 要求 Go 1.25。当前仓库 Go 1.22 与构建镜像必须一起升级，最终使用实施时仍受支持的稳定工具链。

[Eino 现有 MCP 扩展](https://github.com/cloudwego/eino-ext/blob/main/components/tool/mcp/go.mod) 依赖 `mark3labs/mcp-go`，并非官方 SDK。选择官方 SDK 时写最小 Eino Tool 类型桥接，不重写 MCP 协议，不同时引入两套 MCP 栈。

内部 shell/文件工具直接复用 OpenSandbox SDK，外部工具经 MCP；无需为了形式统一增加内部 MCP 网络往返。既有工具名称可通过别名兼容，安全检查、allow-list、Hook 和事件必须覆盖两种路径。MCP 服务描述及返回内容不能作为可信授权依据。

## 5. 长期记忆对比

| 候选及官方来源 | 核验发现 | 选择理由或限制 |
|---|---|---|
| [Mem0 OSS](https://github.com/mem0ai/mem0) | Apache-2.0；独立记忆层、自托管 REST 与 pgvector | 保持用户已选默认；与现有 PostgreSQL 匹配、可渐进接入 |
| [ReMe](https://github.com/agentscope-ai/ReMe) | Apache-2.0；Markdown 来源、归纳、检索、HTTP/MCP；核验 latest v0.4.1.13 | 文件透明和个人经验库方向强备选；服务边界偏本地，需额外隔离 |
| [OpenViking](https://github.com/volcengine/OpenViking) | v0.4.23；知识/记忆/skills 统一目录，分层摘要，Go SDK/HTTP；AGPLv3 | 大公司开源候选中值得跟踪；本期采用会扩大到整个上下文体系迁移 |
| [Graphiti](https://github.com/getzep/graphiti) | 时序知识图谱、事实失效和关系检索；需要图数据库 | 有时间关系查询需求再选，当前无必要增加图基础设施 |
| [ADK Memory](https://github.com/google/adk-docs/blob/main/docs/sessions/memory.md) | 框架服务接口；内存实现与云服务实现有不同能力 | 接口不能替代持久记忆引擎；云托管实现不满足本期全部自托管要求 |
| [Redis Agent Memory](https://github.com/redis/agent-memory-server) | V0 定位开源研究基础，支持的产品路径指向 Iris | 不将研究发行物包装为受支持的开源生产产品 |
| [Letta](https://github.com/letta-ai/letta) | 当前开发迁往 letta-code；旧 V1 server 位于 archive | 有状态 Agent harness 与本项目重叠，不能照旧版教程接入独立记忆服务 |
| [MemOS](https://github.com/MemTensor/MemOS) | Apache-2.0，完整记忆管理方向 | 本轮仅初筛，未同等核验接口与运行成本，不能据宣传断言优于首选 |

### Mem0 的选择条件

已查看 [server/main.py](https://github.com/mem0ai/mem0/blob/main/server/main.py)、[auth.py](https://github.com/mem0ai/mem0/blob/main/server/auth.py) 和 [server 文档](https://github.com/mem0ai/mem0/blob/main/server/README.md)。主分支存在 pgvector 配置、鉴权与 CRUD/search；但认证成功不等于按 AgentForge 的 tenant/user/agent 规则授权。可信后端必须固定检索过滤范围，按 ID 的读写删除也要检查归属，不能直接暴露给浏览器或模型任意调用。

版本必须分清：[仓库 latest](https://github.com/mem0ai/mem0/releases/latest) 此次指向 Node SDK ts-v3.3.1，而 [Python 主分支包声明](https://github.com/mem0ai/mem0/blob/main/pyproject.toml) 为 2.2.1。这不等于 Server 已有同名稳定发行。实施前锁定 Server commit、Python 包和镜像，验证它们的实际组合。

[官方 README](https://github.com/mem0ai/mem0#new-memory-algorithm-april-2026) 说明展示基准含托管版专有优化。选择 OSS 是因为职责和部署匹配，不声称它在本项目中效果最佳。内置服务镜像的模型 provider 集合也不等于整个 Python SDK 的支持范围；GLM/Qwen、自托管 embedding 的端点、输出格式与依赖必须实测。模型服务可继续使用现有配置；组件开源不等于所有模型推理自动离线。

### ReMe、OpenViking 与 Graphiti

[ReMe 服务文档](https://github.com/agentscope-ai/ReMe/blob/main/docs/en/services.md) 明确其 local-first 服务没有通用用户鉴权，默认仅监听 loopback。适合可读可编辑记忆，但作为平台共享服务需处理可信代理、workspace 隔离与并发写入。

[OpenViking v0.4.23](https://github.com/volcengine/OpenViking/blob/v0.4.23/README.md) 以 `viking://` 组织上下文，支持 L0/L1/L2 渐进读取、session 归档和记忆提取。[该版本 LICENSE](https://github.com/volcengine/OpenViking/blob/v0.4.23/LICENSE) 是 AGPLv3，仍然开源；本文件只记录许可证，不将其误标为 Apache 或给出具体法律判断。若未来决定统一 Skill/RAG/Memory，再针对迁移范围和许可要求单独选型。

[Graphiti 安装说明](https://github.com/getzep/graphiti#installation) 要求图数据库，并提醒结构化输出能力不足可能造成摄取失败。其时序关系价值应由实际业务需求驱动，不能仅为了“更高级”引入。

## 6. 仓库接入事实与上一版修正

- `internal/agent/runner.go` 对带 agent_id 的任务使用持久 AgentToolRunner；`internal/sandbox/agent_docker.go` 在 Release 时保留容器。仅更换临时池不会升级 Dashboard 主路径，迁移必须覆盖持久 Agent 与 workspace 浏览。
- 当前 History 按 run_id 保存。长期记忆、conversation/session、run 是不同概念，需要显式确定跨 Run 会话身份。
- `internal/controlplane/service.go` 当前提交固定 local-dev 用户；队列 Task 没有 tenant 字段。第一阶段按单用户、Agent 隔离定义边界，不能宣称已有可靠多租户。
- `internal/rag/embed.go` 是 deterministic hash embedding，适合无密钥测试；真实记忆需独立配置语义 embedding，并记录模型、维度及重建策略。
- 当前工具执行与 sandbox 获取耦合，即使 http_fetch 也先获取沙箱。未来外部 MCP 工具应不必获取 sandbox，shell/文件工具仍须绑定正确 workspace。
- 安全配置不能因服务故障自动降级；长期记忆可在明确可观测的情况下跳过，二者失败策略不同。

以上均为源码检查，未修改任何运行时代码。历史文档仍描述当前实现；本文件与新 ADR 描述目标状态。

## 7. 实施阶段与验收门槛（待执行）

1. **版本与契约验证**：锁定上游源码、依赖与镜像；验证 Go 工具链、模型兼容性、sandbox 后端与网络组合。先做隔离的最小集成，不切默认路径。
2. **沙箱迁移**：通过现有接口接入 OpenSandbox，并明确持久 Agent 的创建、停止、workspace、跨 Run 复用与删除策略。旧 Agent 继续原 provider；新建 Agent 可显式选新 provider。已有 volume 仅通过显式迁移处理，保留回滚数据。
3. **工具循环与 MCP**：Eino 替换内部循环，SDK 工具和 MCP 工具统一经过策略及事件映射。Gateway 工具发现与 Worker 执行使用同一目录视图。保持 gRPC/ACP/REST/SSE 兼容，不自动重试非幂等工具。
4. **长期记忆**：Mem0 服务仅对可信后端开放；运行前检索、运行后持久任务写入；记录来源 Run 和处理状态。范围由后端生成，不能由模型指定。提供查询、更正、删除和导入入口；默认不批量灌入旧历史。
5. **验收及切换**：对新旧路径执行相同契约测试；通过后逐项切换默认配置。每阶段更新开发日志、架构和验收记录，再提交推送。

| 验证对象 | 必须验证的场景 |
|---|---|
| 沙箱 | 同 Run/跨 Run 文件保持、stop/start、服务重启重连、目录浏览一致、超时后实际子进程结束、输出上限、清理失败可见 |
| 隔离与网络 | 确认实际 runtime；拒绝未授权出口；允许域名、重定向/DNS/IPv6路径；凭据不泄漏；不可用时不降级 |
| Tool loop/MCP | 流式参数组装、call ID 对应、Hook deny、allow-list、错误工具名、超时取消、输出截断、SSE 事件顺序、外部工具不获取沙箱 |
| 记忆 | 跨 Run 召回、Agent 隔离、事实更正、删除/遗忘、重复投递、故障重试、中文检索、来源追溯、服务重启恢复 |
| 兼容性 | 现有 GLM/Qwen/mock、CLI、Dashboard、gRPC/ACP、workspace 与旧历史均有回归证据 |

记忆评测固定相同模型、embedding、数据集与 token 预算，比较无记忆、当前检索和 Mem0；记录质量、延迟、写入成本与失败率。沙箱测试记录硬件、OS、runtime、冷热启动及并发条件。未执行前不填写假定的性能数字。

## 8. 文档追溯

本次只新增调研与选型描述，并同步 TODO、文档索引与开发日志。实施跟踪见 [TODO](./TODO.md)，决策摘要见 [ADR 006](./adr/006-open-source-runtime-components.md)。部署验收、真实版本锁定、代码迁移均未完成，不能用于对外宣称已交付能力。
