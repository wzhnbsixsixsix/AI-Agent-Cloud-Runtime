# 开源 Agent Runtime 基础能力选型调研

核验日期：2026-10-09。第 1–8 节为初始调研快照；后续 P0 兼容验证的实际进展、失败与限制见第 9 节。尚未完成真实隔离部署或性能评测，不表示相关能力已经接入 AgentForge 默认业务路径。

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

首轮只新增调研与选型描述，并同步 TODO、文档索引与开发日志。实施跟踪见 [TODO](./TODO.md)，决策摘要见 [ADR 006](./adr/006-open-source-runtime-components.md)。后续 P0 验证见下节，不能将验证入口当作业务能力已经交付。

## 9. P0 实施与验证进展（2026-10-09）

### 固定组合与选择理由

机器可读记录见 [versions.lock.json](../deploy/opensandbox/versions.lock.json)，依赖及校验和见 [独立验证模块](../integration/p0/go.mod) 和 [go.sum](../integration/p0/go.sum)。锁文件仍是 candidate：未核验项使用 `null` 或明确状态，禁止用臆测摘要补齐。

| 项目 | 固定值 | 实际边界 |
|---|---|---|
| Go | 1.26.9 | 当前受支持的 1.26 补丁线；主 module、验证 module、Makefile、两个 Docker 构建阶段一致 |
| Go 镜像 | `golang:1.26.9-alpine` + 锁文件 index digest | 2026-10-10 从固定提交的 Docker 官方 repo-info 补齐摘要；Makefile/两个构建阶段均使用 tag@digest，未拉取或构建 |
| OpenSandbox tag / SDK tag 提交 | `b1a29cf93a823a95913f7943010febb3f29de05c` | `release-1.1.0` 与 `sdks/sandbox/go/v1.1.0` 的 companion tag 指向同一提交 |
| OpenSandbox 镜像构建提交 | `b00c96b0a89c07fc18409fcfe72d170b4c96c8a7` | 上游标签注释明确它是构建提交；不与标签提交混淆 |
| Server / Execd / Egress | 官方 BOM 的三个 sha256，见锁文件 | 配置直接使用 digest；未拉取镜像、未验证发布签名 |
| OpenSandbox Go SDK | v1.1.0 | 模块下载、校验和与编译完成 |
| Eino / 官方模型组件 | v0.9.21 / v0.1.13 | 真实流式文本与工具调用往返验证 |
| 官方 MCP Go SDK | v1.8.0 | 与 Eino 同模块编译及客户端构造通过；未对接真实 MCP 服务 |
| gVisor / Kata 候选 | `release-20261005.0` / `4.2.0` | 官方标签已核验；未安装，二进制摘要和 QEMU 实际配置未验收 |
| Workload 镜像 | `python:3.12.15-slim-bookworm` + 锁文件 index digest | 2026-10-10 从官方 repo-info 冻结；采用 Python + Debian 用户空间供命令/网络探针使用，live 入口检查 bash/Python；镜像尚未实测 |

来源：[Go 稳定版本](https://go.dev/dl/?mode=json)、[OpenSandbox 标签对象](https://api.github.com/repos/opensandbox-group/OpenSandbox/git/tags/836b182e208e66c046026fa0f633e089321f1efb)、[SDK companion tag](https://api.github.com/repos/opensandbox-group/OpenSandbox/git/tags/098659cfcd10e5071670fa6779e0fd72dfec30c6)、[官方 BOM](https://github.com/opensandbox-group/OpenSandbox/releases/download/release-1.1.0/1.1.0.yaml)、[gVisor](https://github.com/google/gvisor/releases/tag/release-20261005.0)、[Kata](https://github.com/kata-containers/kata-containers/releases/tag/4.2.0)。BOM 来源核验不是签名验证，两个标签对象当前报告 unsigned。

曾试验 Go 1.27.2：编译/测试可运行，但 Eino 的 Sonic 依赖发出超出支持范围并回退 `encoding/json` 的警告，因此最终使用 Go 1.26.9。新增框架/SDK 仅位于 `integration/p0`，没有提前替换 Worker 的循环或沙箱 provider。Mem0 Server 的完整版本组合仍随 P1 单独验证，不因本轮 Go 验证而宣称已冻结。

### 验证结果及失败记录（2026-10-09 历史基线）

| 检查 | 结果 |
|---|---|
| 协议生成、本机全部服务构建 | 通过；首次暴露原有 int64 耗时传给 uint64 事件的编译问题，非负钳制后转换修复 |
| Go 1.26.9 全量 `go test -race -count=1 ./...` / `go vet ./...` | 通过；默认跳过的 opt-in live 测试不计为外部验证通过 |
| Linux amd64 / arm64、CGO 关闭的全部服务交叉编译 | 通过，不等于 Linux 运行或 Docker 镜像构建通过 |
| `make p0-check` | SDK、配置/版本一致性、运行时缺失拒绝、SDK 创建失败不自动重试等离线检查通过 |
| 原 Provider：GLM / Qwen | 均有真实文本流、工具调用及结果回填通过记录；GLM 首次失败，单独复测通过 |
| Eino 官方模型组件：GLM / Qwen | Go 1.26.9 下同轮真实测试均通过；早期 GLM 失败尝试未当作通过 |
| 最后一次 `make p0-models` | 原 Provider 的 GLM HTTP 429，Qwen 通过；入口非零退出，未继续该轮 Eino 阶段。不能报告统一入口全绿 |
| P0 Compose `config --quiet` | 占位 API key 下静态校验通过，未创建服务 |
| 显式 live sandbox 入口 | 失败：macOS/arm64 的 Docker socket 不存在；没有 skip 或退回 runc/memory |
| gVisor / Kata 真机网络与服务启动拒绝负例 | 未执行：用户确认没有可用 Linux 主机，当前 macOS 无可用 Docker daemon |

真实模型测试只发送合成提示和 `p0_echo` 假工具，不执行模型返回的 shell，不发送项目文件。每模型每路径至多三个请求；密钥只从进程环境读取，不写入文档/锁文件/日志。失败只输出类型和 HTTP 状态，不输出响应正文。通过样本不表示限流、吞吐或长期稳定性已验收；429 不触发隐藏模型切换或无限重试。

工具链验证还修复 Makefile 对带空格目录的工具目标和目录创建处理。未修改协议定义；生成代码继续按现有规则忽略。没有新增生产 CI，也没有部署或切换主项目服务。

### 沙箱验证边界

[gvisor.toml](../deploy/opensandbox/gvisor.toml) 使用 runsc 与独立 internal IPv4 bridge，不设置 networkPolicy；[kata.toml](../deploy/opensandbox/kata.toml) 使用 kata-qemu、bridge、dns+nft 与关闭 IPv6。internal bridge 只验证“无直接外网”，不能据此宣称已隔绝宿主机和同网段所有资源，生产租户网络隔离仍需评审。

[P0 Compose](../deploy/opensandbox/compose.p0.yml) 与主 Compose 分离，仅适用于专用 Linux 主机。两个 API 绑定 host loopback，缺密钥拒绝启动，不自动重启、不改宿主 runtime。Docker socket 等价于主机管理权限，服务须作为可信控制面，不得直接公开。

真实测试检查 Linux daemon/runtime、固定 Server 镜像和挂载配置；创建后、执行探针前检查实际 workload 的 `HostConfig.Runtime`、非 privileged 和 gVisor 网络。随后检查命令/文件往返、出口与删除后 404。联网检查 **enforcementMode** 为 dns+nft，不把 policy mode 或配置意图当作实际强制模式。独立真实负例要求 server 明确因缺失 runtime 拒绝启动，任意启动异常或超时都不是通过。

Kata 网络探针必须允许 example.com、拒绝 example.org、未授权直连 IPv4 和 IPv6；gVisor 拒绝外部域名与直连地址。仍需记录 runsc/Kata 二进制版本、QEMU 实际配置及运行证据，不能只凭 runtime 注册名判断 hypervisor。重定向、DNS 重绑定及完整威胁模型属于后续安全验收，当前最小探针不构成安全审计。

### 复跑方法与剩余动作

从仓库根目录运行，三个入口均要求本地 Go，防止 Docker 回退时遗漏 opt-in 环境变量而把真实测试静默跳过：

```bash
make p0-check
# 通过现有秘密管理方式注入 MODELSCOPE_BASE_URL / MODELSCOPE_ACCESS_TOKEN / MODELSCOPE_MODEL
make p0-models
```

沙箱测试须在 Docker daemon 所在的专用 Linux 主机运行，不能只把 DOCKER_HOST 指向远端（真实负例需要挂载本地临时配置）。先完成：

1. 安装并核验锁文件的 runtime 候选，注册 runsc/kata-qemu，确认硬件虚拟化、QEMU 后端；不得通过改默认 runtime 绕过错误。
2. 恢复 registry 访问，核验 BOM 签名并按锁文件拉取镜像，对照官方 repo-info 的 Go/workload index 摘要检查实际 manifest 与目标平台。缺失项未补齐前不关闭 P0；不能将官方元数据当作本机镜像验证。
3. 注入 P0_GVISOR_API_KEY / P0_KATA_API_KEY，创建专用网络并启动两套服务：

```bash
docker network create --internal --opt com.docker.network.bridge.enable_icc=false agentforge-p0-offline
docker compose -f deploy/opensandbox/compose.p0.yml up -d
make p0-sandbox
```

已有网络时先检查属性，不删除重建掩盖问题。程序只删除自己创建的 sandbox 和唯一命名负例容器；清理失败报告 ID，TTL 是第二道保护。结束后用同一 Compose 文件 down 停止本次两套服务，不操作主项目服务或 volume。

4. 记录真实 runtime、启动拒绝、网络探针、清理结果；Qwen 访问恢复后重跑统一模型入口。完成后更新 TODO/开发日志再进入 P1；当前两个 P0 均保持未完成。

### 2026-10-10 更新：移除 GLM，不再等待 429 恢复

按用户明确要求，撤销 GLM 兼容性验收要求并删除其配置。默认 Provider、Dashboard/API 模型均改为 ModelScope Qwen；本地 `.env` 的 GLM 项已清除（不提交密钥）。统一真实模型入口只覆盖 Qwen，仍验证原 Provider 与 Eino 的流式文本、工具参数和结果回填，不用 mock 代替真实调用。

保留 OpenAI-compatible 协议实现是因为 Qwen/Eino 仍使用该协议，不代表保留 GLM Provider。旧 `LLM_PROVIDER=openai` 明确报错，未配置模型不会发送到 ModelScope；已有 GLM Agent、历史和 workspace 不自动改写或删除，需新建 Qwen Agent（当前没有编辑接口）。以上历史成功和失败记录保留供追溯，不作为最新通过结论。

本轮 `make p0-models` 的原 Provider Qwen 返回 HTTP 403，未进入同轮 Eino 阶段；另行执行 Eino 也返回 HTTP 403。原因尚未确认，详情不入日志；未将 403 当作 GLM 429 忽略，亦未自动换模型。

Docker Hub registry 再次连接超时，但 GitHub 官方元数据可读取：已从 Docker 官方 repo-info 提交 `9dc44abcb57cd70ab4a7c5fdd4a4ea3cd1338d3d` 核验 [Go index 摘要](https://github.com/docker-library/repo-info/blob/9dc44abcb57cd70ab4a7c5fdd4a4ea3cd1338d3d/repos/golang/remote/1.26.9-alpine.md) 与 [Python workload index 摘要](https://github.com/docker-library/repo-info/blob/9dc44abcb57cd70ab4a7c5fdd4a4ea3cd1338d3d/repos/python/remote/3.12.15-slim-bookworm.md)，写入锁文件和 Go 构建引用。选择多架构 index 保留 amd64/arm64 构建能力，Python slim-bookworm 避免自行维护测试镜像；实际 bash/Python 可用性仍由 live 入口核验。来源记录可追溯不等于 manifest 下载、镜像拉取或签名已验证。

Docker socket 仍不存在且无 Linux 主机：镜像拉取与供应链核验、真实 gVisor/Kata QEMU 验收仍为阻塞项。不能把已有离线测试通过等同于两项 P0 完成。
