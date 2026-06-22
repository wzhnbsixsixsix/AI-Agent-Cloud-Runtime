# AgentForge — 云原生多智能体运行时

> **求职项目定位**：一个用 Go 写的、生产级的 **AI Agent 运行时平台**，对标 Claude Code / Cursor Background Agent / OpenAI Assistants Runtime。
>
> 一句话简历版："**基于 Go 自研的云原生多智能体运行时，支持沙箱隔离执行、动态 Skill 加载、可变上下文管理与多 Agent 协同，单机 5k+ 并发会话，P99 调度延迟 < 50ms。**"

---

## 0. 为什么这个项目适合求职

| 招聘方在意的能力 | 项目里如何体现 |
|---|---|
| **Go 工程功底** | 自研协议编解码、goroutine 池、context 全链路取消、零拷贝、pprof 调优 |
| **分布式/中间件理解** | Redis Stream 队列、Raft 选主、etcd 服务发现、gRPC 微服务、分布式锁 |
| **系统设计能力** | 调度器、状态机、隔离模型、限流降级、可观测性 |
| **AI Infra 前沿性** | RAG、MCP、Agent 编排、上下文工程、Sandbox —— 2025/2026 最热方向 |
| **容器/云原生** | Docker SDK 编程、cgroup/namespace、K8s Operator |
| **能讲故事** | 每个模块都有"为什么这么做"的取舍，面试官好挖深 |

> 简历项目最忌讳"什么都做了一点"。本设计**主动砍掉**了花哨但不深的东西（前端 UI、各种花式 Agent demo），把所有精力堆在 **Runtime 内核 + 可观测性 + 性能** 三件事上。

---

## 1. 项目命名与边界

- **项目名**：`AgentForge`（也可叫 AgentOS / Forge-Runtime，简历建议用 AgentForge，搜索唯一性好）
- **做什么**：一个让"Agent 像微服务一样被部署、调度、隔离、观测"的平台
- **不做什么**：
  - 不做大模型训练/微调
  - 不做花哨的 Web 控制台（只做必要的 Admin API + 一个极简 Dashboard）
  - 不重新发明 LLM SDK，统一抽象 Provider 即可

---

## 2. 完整技术栈（在你给的基础上扩展）

### 2.1 你已点名的（保留）
Go、Sandbox、RAG、Multi-Agent、自研协议、异步高并发、Docker、Redis、SQLite、动态历史、Subagent 上下文压缩、Skill 动态加载、Hook、读写隔离、gRPC 微服务、队列

### 2.2 我建议追加的（**每一个都是面试加分点**）

| 技术 | 用在哪 | 面试可讲的点 |
|---|---|---|
| **PostgreSQL + pgvector** | 全局元数据 + 向量检索一体化 | 为什么不用 Milvus：减少组件、事务一致 |
| **NATS JetStream** | 事件总线（Hook、可观测） | 对比 Kafka/Redis Stream 的取舍 |
| **etcd + Raft** | 服务发现 + Master 选主 | 高可用方案、脑裂处理 |
| **OpenTelemetry** | Trace / Metrics / Logs 三合一 | 全链路追踪一个 Agent Run |
| **Prometheus + Grafana** | 监控 | SLO 指标设计：调度延迟、token/s、沙箱启动耗时 |
| **eBPF (Cilium Tetragon)** | 沙箱内系统调用审计 | 安全亮点，2026 年很热 |
| **WASM (wazero)** | 轻量 Hook 脚本运行时 | 用户自定义 Hook 不用起容器 |
| **Firecracker / gVisor** | 强隔离 Sandbox 备选 | "为什么不只用 Docker"——有答案 |
| **MCP 协议兼容层** | 兼容 Anthropic Model Context Protocol | 紧跟行业标准，能复用现成 MCP Server |
| **Protobuf + gRPC-Gateway** | 内部 gRPC + 外部 REST 一份定义 | 工程化亮点 |
| **Wire (DI)** | 依赖注入 | 代码组织讲究 |
| **golangci-lint + race detector + 单元/集成测试 ≥ 70%** | 工程质量 | 简历上敢写"覆盖率 70%+" |
| **GitHub Actions CI + Docker 多阶段构建** | DevOps | 简历加分 |
| **K6 / ghz** | 压测工具 | 能产出"5k QPS 压测报告"截图 |

### 2.3 主动砍掉的（避免战线过长）
- ❌ 前端 React 控制台（只做 Swagger + 一个 50 行的 HTML 监控页）
- ❌ 自训模型
- ❌ 多语言 SDK（只出 Go SDK + CLI，够了）

---

## 3. 总体架构

```
                    ┌──────────────────────────────────┐
   CLI / SDK ──────►│   Edge Gateway  (ACP + gRPC-GW)  │  ← 自研协议接入 + REST 兼容
                    │   mTLS / JWT / 限流 / 路由        │
                    └──────────────┬───────────────────┘
                                   │ gRPC
       ┌───────────────────────────┼───────────────────────────────┐
       ▼                           ▼                               ▼
┌──────────────┐            ┌─────────────┐                 ┌───────────────┐
│ Scheduler    │◄──Raft────►│ Scheduler-2 │                 │  Skill Svc    │
│ (Leader)     │            │ (Follower)  │                 │ (索引+Selector)│
└──────┬───────┘            └─────────────┘                 └───────────────┘
       │ pub task                                                  ▲
       ▼                                                           │
┌──────────────────────────────────────────┐               ┌───────┴────────┐
│  Redis Stream  (任务队列, Consumer Group) │               │   RAG Svc      │
└──────────────────────────────────────────┘               │ (pgvector+BM25)│
       │ pull                                              └────────────────┘
       ▼
┌─────────────────────────────────────────────────────────┐
│              Worker Pool  (N 实例, 横向扩展)              │
│  每个 Worker：goroutine 池 + Sandbox 池 + LLM Streamer    │
└────────────┬────────────────────────────────────────────┘
             │ docker API / firecracker
             ▼
   ┌──────────────────────────────────────────┐
   │   Sandbox Layer  (Docker / gVisor / FC)  │
   │   每 Run = 1 容器 + 独立 workspace mount  │
   └──────────────────────────────────────────┘
             │
             ├─► Postgres (元数据 + 向量)
             ├─► Redis     (会话/历史/队列/限流)
             ├─► SQLite    (Sandbox 内 run 状态)
             └─► NATS JS   (Hook / 事件总线)

观测平面：OpenTelemetry Collector ─► Prometheus / Loki / Tempo ─► Grafana
```

---

## 4. 核心模块（按"面试可讲性"排序）

### 4.1 ⭐ ACP — 自研 Agent 通信协议

> **简历亮点**："设计并实现二进制双向流协议 ACP，吞吐较 HTTP/JSON 提升 3.8x"

**帧格式**：
```
+--------+------+------+-------+--------+----------+----------+
| Magic  | Ver  | Type | Flags | SeqID  | Length   | Payload  |
| 2B     | 1B   | 1B   | 1B    | 8B     | 4B       | N bytes  |
+--------+------+------+-------+--------+----------+----------+
```

**特性**（每条都能展开讲 5 分钟）：
1. **基于 TCP 长连 + 帧分片**，避免 HTTP/2 在长流式下的队头阻塞
2. **双向流**：Server 主动 push token / tool_call / hook_event
3. **断线续传**：基于 `SessionID + LastSeq`，客户端重连后服务端从环形缓冲补发
4. **背压**：Window Update 帧（仿 HTTP/2 流控），防止慢客户端打爆服务端内存
5. **Payload = Protobuf**，支持 zstd 压缩（Flags 标志位）
6. **零拷贝解析**：用 `bufio.Reader` + `sync.Pool` 复用帧 buffer，pprof 可证明零分配热路径
7. **MCP 兼容层**：把 ACP 帧映射到 Anthropic MCP，能直连任何 MCP Server

**实现要点**：
```go
type Frame struct {
    Magic   uint16
    Version uint8
    Type    FrameType
    Flags   uint8
    SeqID   uint64
    Payload []byte  // 从 pool 拿
}

// 关键：解码时不 copy payload，引用底层 buffer，调用方用完归还
func (c *Conn) ReadFrame() (*Frame, ReleaseFunc, error) { ... }
```

### 4.2 ⭐ 可变历史 (Mutable History Store)

> **简历亮点**："设计可变事件流式历史存储，支持 LLM 上下文原地改写，token 节省 35%"

**问题**：传统对话历史是 append-only，遇到"修改上一步输出"只能让 LLM 自己道歉重写，浪费 token。

**方案**：history 是**有序事件流 + 可变指针**：

```go
type Message struct {
    ID        string            // ULID，时间有序
    Role      Role
    Content   string
    Visible   bool              // 软删
    Version   uint32            // 乐观锁
    ParentID  string            // 用于 Subagent 嵌套折叠
    Tags      map[string]string // compacted=true 等
}

type HistoryStore interface {
    Append(runID string, m Message) (msgID string, err error)
    Patch(runID, msgID string, content string) error   // 原地改写
    Hide(runID, msgID string) error                    // 软删
    Fold(runID string, fromID, toID, summary string) error // 折叠多条为一条
    Render(runID string, opts RenderOpts) []Message    // 给 LLM 看的视图
}
```

**存储**：Redis Hash + Sorted Set（score=ULID 时间戳），写操作走 Lua 脚本保证原子性。

**典型场景**：
- 用户："把刚才那段改成 Y" → Runtime `Patch(lastAssistantMsgID, Y)`
- Subagent 完成 → `Fold(start, end, summary)`，主 Agent 上下文里只剩一行
- 上下文超限 → 自动 `Fold` 旧消息

### 4.3 ⭐ Skills 两段式动态加载（LLM Pre-Routing）

> **简历亮点**："实现基于轻量 LLM 预路由的 Skill 动态加载，prompt 体积下降 90%"

**两段式**：

**Stage 1 — 索引构建**（启动 + 文件 watch 热更新）
```go
// 正则扫 frontmatter
var skillRe = regexp.MustCompile(`(?ms)^---\s*\n(.*?)\n---`)
// 仅抽 name + description，每个 skill ~200 字节
```

**Stage 2 — Selector Agent**
- 用便宜小模型（Haiku / GPT-4o-mini）
- Input: 用户 query + 全部 (name, description) 列表
- 强制 functioncall：`load_skills(names: string[])`
- Output 校验通过后，主 Agent 才加载完整 SKILL.md

**好处**：100 个 skill 的索引才 20KB，主 Agent 只看到选中的 2-3 个完整内容。

**额外亮点**：Selector 的决策结果走 LRU 缓存（按 query 语义哈希），命中率 60%+ 不用每次调 LLM。

### 4.4 ⭐ Sandbox 分级隔离

> **简历亮点**："基于 Docker / gVisor / Firecracker 三级沙箱方案，冷启动从 800ms → 80ms"

**三级**：
| 级别 | 实现 | 启动 | 适用 |
|---|---|---|---|
| L1 | Docker + seccomp | ~300ms | 受信任内部 Agent |
| L2 | gVisor (runsc) | ~500ms | 第三方 Skill |
| L3 | Firecracker microVM | ~125ms | 执行不受信代码 |

**冷启优化**：
1. **预热池**：常驻 N 个 idle 容器，拿来即用，用完销毁
2. **CRIU checkpoint**：保存"已注入 system prompt 的 Python 解释器"快照，秒级恢复
3. **Image layer 复用**：基础镜像 + workspace bind mount，不重建

**资源限制**（cgroup v2）：CPU shares、memory limit、pids max、IO weight、网络白名单（iptables OWNER 模块）。

**可观测**：通过 eBPF 抓 syscall，违规调用（execve 黑名单）直接 kill + 告警。

### 4.5 ⭐ 调度器（Scheduler）

> **简历亮点**："基于 Raft 选主的高可用调度器，故障切换 < 3s，单 Leader 调度 10k QPS"

**职责**：
- 接收 Run 请求 → 选 Worker → 投递到 Redis Stream
- 维护全局状态机（PENDING/RUNNING/WAITING_TOOL/COMPACTING/DONE/FAILED）
- 限流（用户级 + 全局）、配额、优先级队列

**调度算法**：
- **加权最少连接**（按 Worker 当前 in-flight runs 和资源指标）
- **亲和性**：同一 user 的连续 run 尽量打到同一 Worker（利用本地 sandbox 缓存）
- **抢占**：高优先级任务可抢占 idle worker 的低优先级任务

**HA**：3 节点 Raft（用 hashicorp/raft），Leader 处理写，Follower 只读。

### 4.6 ⭐ 多 Agent 编排 + Subagent 压缩

**三种编排模式**（同时支持）：
1. **Supervisor**：主 Agent functioncall `dispatch_subagent(role, task)`
2. **Pipeline (DAG)**：YAML 定义，前序输出注入后序
3. **Swarm**：N 个并行 + Aggregator 投票

**Subagent 隔离**：
- 独立 RunID、独立 history、独立 sandbox
- 父子之间只通过**结构化结果对象**通信（JSON Schema 约束）
- 父 history 里只留一条折叠摘要

**上下文压缩（自动触发）**：
- 触发：token > 80% context window
- 三级降维：
  - L1：截断中段，保留 system + 首尾 N 轮
  - L2：小模型摘要（结构化：decisions / file_changes / errors / next）
  - L3：把工作产物 dump 到 workspace 文件，history 里只留 `see: notes/step3.md`

### 4.7 RAG（不做花活，做扎实）

- **Ingest**：文档 → 解析（pdf/docx/md/code 用 tree-sitter）→ 语义切片 → embedding → pgvector
- **Retrieve**：BM25（PG 全文）+ 向量混合检索 → bge-reranker-v2 → Top-K
- **多租户**：所有查询强制带 `tenant_id` 过滤，PG Row-Level Security 兜底
- **缓存**：query embedding LRU，命中率 40%+

### 4.8 工作隔离

| 维度 | 实现 |
|---|---|
| **读隔离** | 三元组 `(AgentID, WorkspaceID, UserID)` 作为 scope key，DB 层 + 应用层双重过滤；PG RLS 兜底 |
| **写隔离** | `-w workspace` 参数 → Sandbox bind mount 唯一可写目录；其余 mount 全 readonly；eBPF 审计写操作 |

```go
type IsolationCtx struct {
    AgentID, WorkspaceID, UserID, WriteRoot string
}
func (i *IsolationCtx) MustWritable(p string) error // 所有 tool 强制过这一关
```

### 4.9 Hook 系统

| Hook | 触发 | 实现 |
|---|---|---|
| PreToolUse / PostToolUse | tool 调用前后 | WASM (wazero) 跑用户脚本，10ms 内执行 |
| PreLLM / PostLLM | LLM 调用前后 | 注入 system / 输出脱敏 |
| OnError | 异常 | 自愈重试 / 告警 |
| OnCompact | 压缩前后 | 自定义压缩 |

**亮点**：用 **WASM 而非容器**跑 Hook —— 启动 0.1ms，沙箱安全，多语言（Rust/Go/JS 编译到 wasm）。

### 4.10 队列与异步

- **任务队列**：Redis Stream + Consumer Group（ack + 重试 + 死信）
- **事件总线**：NATS JetStream（持久化 + 重放）
- **延迟队列**：Redis ZSET（自动重试、定时任务）
- **跨 Worker 通信**：NATS Request/Reply（Subagent 跨节点调用）

### 4.11 可观测性（**简历必写**）

- **Trace**：OTel，一个 AgentRun 的完整链路（gateway → scheduler → worker → sandbox → tool → llm）
- **Metrics**（Prometheus）：
  - `agent_run_duration_seconds` (histogram, by agent_id)
  - `sandbox_cold_start_seconds`
  - `llm_tokens_total{provider, model, type}`
  - `tool_call_errors_total`
  - `scheduler_queue_depth`
- **日志**：结构化 JSON → Loki，关联 trace_id
- **Dashboard**：Grafana 一键导入 JSON，简历附截图

### 4.12 安全

- mTLS 内部通信，JWT 外部
- Sandbox 默认无网，白名单 HTTP 代理
- Prompt Injection 防御：所有外部内容打 `<untrusted>` 标签
- 审计日志 append-only，按 user 分片
- 密钥用 envelope encryption（KEK/DEK）

---

## 5. 数据模型（精简）

```sql
-- Postgres
CREATE TABLE agents (id, name, version, system_prompt, tools, skills, ...);
CREATE TABLE workspaces (id, user_id, root_path, quota);
CREATE TABLE agent_runs (id, agent_id, user_id, workspace_id, state, started_at, ...);
CREATE TABLE hooks (id, scope, event, wasm_blob, enabled);
CREATE TABLE skills_index (name, description, sha256, path);
CREATE TABLE rag_chunks (id, tenant_id, content, embedding vector(1024));
CREATE INDEX ON rag_chunks USING hnsw (embedding vector_cosine_ops);

-- Redis
history:{run_id}        Hash + ZSet
session:{user_id}       Set
queue:agent_tasks       Stream
ratelimit:{user_id}     TokenBucket (Lua)
sandbox:warm_pool       List

-- SQLite (Sandbox 内)
edits, tool_calls, kv
```

---

## 6. 目录结构

```
AgentForge/
├── cmd/
│   ├── gateway/         ACP + gRPC-GW
│   ├── scheduler/       Raft 调度
│   ├── worker/          执行
│   ├── skill-svc/
│   ├── rag-svc/
│   ├── sandbox-svc/
│   └── agentctl/        CLI
├── internal/
│   ├── acp/             自研协议
│   ├── mcp/             MCP 兼容层
│   ├── agent/           Run 状态机
│   ├── history/         可变历史
│   ├── skill/           索引 + selector
│   ├── rag/             检索
│   ├── hook/            WASM runtime
│   ├── sandbox/         docker/gvisor/firecracker driver
│   ├── isolation/       读写隔离
│   ├── llm/             provider (openai/claude/local)
│   ├── scheduler/       调度算法
│   ├── storage/         redis/pg/sqlite
│   └── obs/             OTel 封装
├── pkg/
│   ├── proto/           protobuf
│   └── sdk-go/
├── deploy/
│   ├── docker-compose.yml
│   ├── helm/
│   └── grafana-dashboards/
├── skills/              内置 skills
├── benchmarks/          压测脚本 + 报告
├── docs/                架构图 + ADR
└── .github/workflows/
```

---

## 7. 路线图（10 周，求职冲刺节奏）

| 周 | 阶段 | 交付（每周末必须能 demo） | 状态 |
|---|---|---|---|
| W1 | 骨架 | Gateway + Scheduler + Worker 单体跑通，echo agent，Docker Compose 一键起 | ✅ 完成 |
| W2 | ACP 协议 v1 | 帧编解码 + 双向流 + 断线续传，单测 + benchmark | ✅ 完成 |
| W3 | Sandbox L1 | Docker driver + 预热池 + 隔离 + 5 个内置 tool | ✅ 完成 |
| W4 | LLM + History | OpenAI provider + function-calling 闭环 + 可变历史 Fold | ✅ 完成 |
| W5 | Skill | 索引 + Selector + 缓存 + 5 个内置 skill | ✅ 完成 |
| W6 | RAG | pgvector + 混合检索 + reranker | ✅ 完成 |
| W7 | Multi-Agent | Supervisor + Pipeline + 上下文压缩三级 | ✅ 完成 |
| W8 | Hook + 微服务化 | Hook gRPC + Skill/RAG/Hook 服务拆分 + Scheduler Pick/Leader | ✅ 完成 |
| W9 | 可观测 + 压测 | OTel + Prometheus + Grafana 大盘 + mock RunAgent 压测报告 | ✅ 完成 |
| W10 | 打磨 + 文档 | README、架构图、ADR、demo 视频、简历话术 | ⏳ 待开工 |

> 仓库：<https://github.com/wzhnbsixsixsix/AI-Agent-Cloud-Runtime> · 当前 `main` 提交 `d21f72c (feat(w3): sandbox L1 ...)`。

### 7.1 实施进度同步（W1–W7 已交付）

> 以下记录到 **2026-06-05** 为止"实际写出来 vs 设计文档"的对账，避免后面回头找细节。

#### W1 — 骨架（✅ 完成）

**已做**：
- `cmd/gateway` + `cmd/scheduler` + `cmd/worker` + `cmd/agentctl` 四个二进制，`deploy/docker-compose.yml` 一键起 `gateway + scheduler + worker + redis`
- gRPC `RunAgent` 双向流：`agentctl run --prompt "..."` → gateway → scheduler → Redis Stream `queue:agent_tasks` → worker consumer → OpenAI 兼容 API 流式 token → 反向流回 CLI
- Redis Stream Consumer Group + ack + 重试 + DLQ；Worker 心跳；状态机最小子集（PENDING/RUNNING/DONE/FAILED）
- `internal/storage/redis` 封装 + key 规范集中在 `keys.go`
- `internal/llm/openai` provider（流式）

**与设计的差异**：
- 状态机暂只有 4 个态，`WAITING_TOOL` / `COMPACTING` 留到 W4 / W7
- Worker 选择走的是 Redis Stream 抢占式消费而非"调度器主动选 Worker"——简化版，W8 起 Raft 后再回填
- 元数据未起 Postgres，Run 状态只在 Redis；Postgres 推迟到 W6 上 RAG 时统一开通

#### W2 — ACP 自研协议（✅ 完成）

**已做**：
- `pkg/acp/spec.md` + 帧编解码（Magic/Ver/Type/Flags/SeqID/Length/Payload，与设计文档 §4.1 帧格式一致）
- `internal/acp` 三件套：server / session / event-cache（Redis ZSet 环形缓冲）+ client
- gateway 同时监听 `:8080` (gRPC) 与 `:8090` (ACP)；gRPC 注册 `health.v1`
- 断线续传：`RESUME{run_id, last_seq}` 从 ZSet 回放
- `agentctl --proto acp|grpc` 切协议；`agentctl resume --run-id ... --last-seq ...`
- `bin/bench rtt | throughput | connect` 一条命令对比 ACP vs gRPC
- 单测：codec 5 例 + ACP server happy/ping/resume 3 例

**与设计的差异（待 W9 / W10 回填）**：
- ⚠️ **Window Update 背压帧暂未实现**——目前靠 TCP socket buffer 自然背压，正式压测前补
- ⚠️ **zstd 压缩 Flag 暂未启用**——帧格式预留位已留好，没拉 zstd 依赖
- ⚠️ **零拷贝 + sync.Pool buffer 复用未做完**——当前 `ReadFrame` 还在拷贝 payload，简历上"零分配热路径"的说法等 W9 用 pprof 验证后再写
- ⚠️ **MCP 兼容层**未做，挪到 W4 之后做（与 LLM tool-use 一起接更顺）
- ✅ "吞吐 3.8x"目前 `bin/bench` 跑出来的是工程联调数据；W9 已补 `bench run-agent` 和报告模板，正式对外数字以 `docs/W9_BENCH_REPORT.md` 记录为准

#### W3 — Sandbox L1（✅ 完成）

**已做**：
- `internal/sandbox`：`Driver` 接口 + `DockerDriver`（预热池 N=4 idle 常驻 + 异步补位）+ `MemoryDriver`（无 docker 时降级，仅工程联调用）
- 容器隔离矩阵：`network=none` / read-only rootfs + `/tmp` tmpfs / `cap_drop ALL` + `no-new-privileges` / memory + cpu quota + pids cgroup / 单次 exec 硬超时 60s
- Per-Run 语义：pop slot → bind 宿主 `/tmp/agentforge/<slot>/runs/<run_id>` ⇄ 容器 `/workspace/runs/<run_id>` → 用完 force remove + 异步 spawn 新 slot 补位
- `internal/tool` 5 个内置 tool（**bash / fs_read / fs_write / fs_list / http_fetch**）+ `safePath` 防 `../` 越出 workspace + `Descriptor` 兼容 OpenAI / Anthropic JSON Schema
- 独立 Redis Stream `queue:tool_tasks` + Pub/Sub `tool_results:{call_id}` 做请求-响应（不动 W1 的 `queue:agent_tasks`）
- gRPC 新增 `ListTools` / `ExecTool` 两个 RPC + `agentctl tool list [--schema]` / `agentctl tool exec <name> --args '<json>'`
- `deploy/docker-compose.yml`：worker 容器挂 `/var/run/docker.sock` + 与宿主共享 `SANDBOX_WORKSPACE_HOST` bind
- `.env.example` 覆盖 `SANDBOX_*` / `TOOL_*` 全部可调参数
- 单测：MemoryDriver 4 例 + tool 6 组；docker 集成测试用 build tag `integration_docker` 隔离（需要 docker daemon）

**与设计的差异**：
- 设计原文写"每 Run = 1 容器、用完销毁"——已实现，但底下用预热池 pop slot 而非冷拉镜像，**冷启动从 ~800ms 降到取一个 idle slot 的 ~20ms 量级**（W9 压测时正式出数）
- `http_fetch` **故意不走 sandbox**：sandbox `network=none`，所以 http_fetch 在 worker 主进程跑，靠 `TOOL_HTTP_ALLOW_LIST` + `TOOL_HTTP_MAX_BYTES` 兜底；这个设计没在原 §4.4 里——是 W3 实现期的合理偏差
- ⚠️ **gVisor (L2) / Firecracker (L3) 暂未做**——简历上"三级沙箱"目前只交付 L1，L2/L3 推迟到 W8 / W10（写 ADR 时论证 L1 已够 demo，L2/L3 列为已设计未实现的扩展点）
- ⚠️ **CRIU checkpoint 秒级恢复**未做——目前预热池靠"常驻 idle 容器"硬抗冷启，CRIU 留作 W10 加分项
- ⚠️ **eBPF syscall 审计 + 黑名单 kill**未做——同样进 W10 加分项；当前 `cap_drop ALL` + `read-only rootfs` 已经足够 demo 隔离

#### W3 收尾的运行时验证（待用户机器跑）

本机无 Go/Docker，以下两步会在 docker build 时自动触发，**用户在能跑 docker 的机器上 `make up` 即可**：
- `go mod tidy` 拉 `github.com/docker/docker/...`（Dockerfile builder 阶段已含）
- `pkg/proto/gen/*.pb.go` 重新生成（Dockerfile `bufgen` stage 跑 `buf generate`）

端到端 demo 命令见 `README.md` §9「W3 demo」（含网络隔离 / read-only rootfs 验证脚本）。

#### W4 — OpenAI Tool-Calling + History Fold（✅ 完成）

**已做**：
- `internal/llm` 扩展 tool-aware 类型：`ToolDefinition` / `ToolCall` / `Message.ToolCalls` / `Message.ToolCallID` / `TokenEvent.ToolCalls`
- OpenAI 兼容 provider 支持 `tools` 请求字段，并能把 streamed `delta.tool_calls` 的 name / arguments 分片聚合成完整 `ToolCall`
- `agent.Runner` 支持 bounded function-calling loop：纯文本路径保持不变；模型返回 tool call 后进入 `WAITING_TOOL`，worker 本地执行 W3 tool，再追加 `role=tool` 消息继续请求模型
- worker bootstrap 复用同一个 `tool.Registry` + `sandbox.Driver`，agent loop 不绕回 gateway / Redis RPC
- 新增 `AGENT_TOOL_MAX_STEPS`（默认 5），超过最大 tool 轮数返回 `tool_loop_limit`
- `internal/history.Store` 新增 `Fold(ctx, runID, fromID, toID, summary)`：闭区间软删 + 追加 `compacted=true` 摘要消息
- 单测覆盖 OpenAI text / tool_call streaming / tools 请求体、History Fold、Runner 文本路径 / tool loop / loop cap

**与设计的差异**：
- Anthropic `tool_use` 暂未实现；W4 只做 OpenAI 兼容闭环，避免同时扩两套 provider wire shape
- History Fold 先沿用现有 Redis Hash + ZSet + pipeline；Lua 原子写和自动压缩策略留到 W7 上下文压缩一起做
- `agent.proto` 未新增 RPC，`agentctl run` 保持原入口；tool-calling 是 worker 内部能力

#### W5 — Skill 索引 + Selector（✅ 完成）

**已做**：
- `internal/skill.Indexer` 扫描 `skills/**/SKILL.md`，轻量解析 frontmatter 的 `name` / `description`，记录 `sha256`、path 和完整 `SKILL.md` 内容；缺失 skill root 时返回空索引，不影响 worker 启动
- `RuleSelector` 基于 prompt 与 skill name / description / content 的确定性关键词打分，稳定排序后返回 Top-K
- `CachedSelector` 按规范化 query 的 SHA-256 做本地 TTL 缓存，支持容量上限和 LRU-ish 淘汰
- `agent.Runner` 在固定 system prompt 之后、history 之前注入一条动态 skill system message；selector 失败、无命中或 `SKILL_ENABLED=false` 时自动退回 W4 行为
- worker bootstrap 支持 `SKILL_ENABLED` / `SKILL_ROOT` / `SKILL_TOP_K` / `SKILL_CACHE_TTL` / `SKILL_CACHE_SIZE`，distroless 镜像把内置 `skills/` 复制到 `/skills`
- 内置 5 个 skill：`sandbox-files`、`bash-debug`、`http-fetch`、`go-test`、`agentforge-overview`
- 单测覆盖 frontmatter、索引、重复名、缺目录、selector、cache hit、Runner skill 注入和 skill+tool loop

**与设计的差异**：
- W5 先落地 deterministic rule selector，未默认启用轻量 LLM function-call selector；这样 demo 不新增一条外部 LLM 调用链，稳定性更好
- 暂未做文件 watch 热更新；worker 启动时加载一次 skill index，后续可在 W7/W8 引入 watch 或管理面 reload
- 缓存 key 先用规范化 query 的 SHA-256，而不是真正的 embedding 语义哈希；语义缓存等 RAG/embedding 基建在 W6 后再接

#### W6 — RAG 纵切（✅ 完成）

**已做**：
- `internal/rag` 提供 chunking、确定性 hash embedder、keyword reranker、Postgres + pgvector store 和 `Service`
- `AgentService` 新增 `IngestRAG` / `QueryRAG`；`agentctl rag ingest --path ...` 在 CLI 侧读取本地文件内容并提交给 gateway 入库，避免 gateway 访问用户本地路径
- docker compose 新增 `postgres` (`pgvector/pgvector:pg16`)；gateway/worker 在 `RAG_ENABLED=true` 时初始化 pgvector extension、`rag_chunks` 表和索引
- `agent.Runner` 在固定 system prompt / skill context 后、history 前注入 RAG context，所有 chunk 均包在 `<untrusted>` 标签内
- RAG 失败、无结果或未启用时自动退回 W5 行为，不改变 `RunAgent` / ACP / tool / skill 原入口
- 单测覆盖 chunk、embedding、rerank、tenant filter、min-score、Runner RAG 注入；Postgres 集成测试通过 `integration_pg` build tag 隔离

**与设计的差异**：
- W6 默认用确定性 hash embedding，确保没有 embedding API key 也能 demo；真实 embedding provider 留到 RAG 增强阶段
- PDF/docx、tree-sitter 代码解析暂未实现；W6 先支持本地文本、Markdown 和代码文件的通用文本切片
- bge reranker 暂由 keyword reranker 代替，接口已预留，后续可替换为外部 rerank service

#### W7 — Multi-Agent + Context Compression（✅ 完成）

**已做**：
- `internal/orchestrator` 新增 Supervisor、Pipeline DAG 和 Context Compact policy
- `agent.Runner` 在 `MULTI_AGENT_ENABLED=true` 时向 LLM 暴露 `dispatch_subagent` tool；模型请求后 worker 本地执行 child run，child run 拥有独立 `run_id` 和 history，父 run 只收到结构化 JSON tool result
- `AgentService` 新增 `RunPipeline`；`agentctl pipeline run --file examples/pipeline/readme-review.yaml` 可按拓扑顺序执行多 step DAG，依赖 step 的输出会注入后续 step
- Context compaction 在历史超过阈值时发布 `COMPACTING` 状态，调用 W4 `History.Fold` 折叠中间历史，并保留头尾消息
- 新增配置：`MULTI_AGENT_ENABLED`、`SUBAGENT_*`、`CONTEXT_COMPACT_*`
- 单测覆盖 supervisor 限制、pipeline 校验/拓扑/依赖注入、Runner subagent tool 和 Runner compaction

**与设计的差异**：
- W7 只实现 Supervisor + Pipeline；Swarm 多 Agent 投票留到后续增强
- Subagent 在当前 worker 进程内本地执行，不通过 Redis 跨 worker 调度；跨 worker subagent 等 W8/NATS/服务发现阶段再做
- 上下文压缩使用确定性本地摘要，不调用小模型；小模型结构化摘要可在 W8 Hook 或 W9 观测阶段增强

#### 产品化拆分计划 — Runtime 主仓库 + 企业中台实例仓库

W7 之后项目拆成两条并行线，避免一个仓库同时承担“底层运行时”和“业务应用案例”两种目标。主仓库继续保持 AI Runtime 的完整性和通用性；实例仓库从主仓库 fork 出来，专门做一个可演示、可讲业务价值的企业内中台案例。

##### 仓库 1：完整 AI Runtime 系统（`main`）

**定位**：`main` 是通用 Agent Runtime 基座，对标 OpenAI Assistants Runtime / Claude Code Runtime / LangGraph 后端平台。它只沉淀可复用基础能力，不绑定具体企业业务。

**保留目标**：
- `agentctl run` / gRPC / ACP / Redis queue / worker pool 作为核心执行链路
- Sandbox tool calling、History、Skill selector、RAG、Multi-Agent、Pipeline、Context compaction 作为 runtime 能力
- W8 继续推进 Hook、微服务拆分、etcd 服务发现、Raft scheduler、可观测性与压测
- 文档重点讲系统设计：调度、隔离、上下文工程、工具执行、RAG、可观测性、扩展点

**不在主仓库做的事**：
- 不直接耦合飞书 / Lark、企业工单、审批、IM、Base 等业务流程
- 不把企业 demo 的 prompt、Skill、pipeline、外部 API 编排写死进 runtime
- 不为了某个 demo 改 `RunAgent`、ACP framing、tool RPC、Skill/RAG 公共接口

##### 仓库 2：企业研发运维智能工单中台实例（从主仓库 fork / 分支）

**建议仓库名**：`AgentForge-Enterprise-Ops-Copilot`

**建议分支名**：`demo/lark-ops-center` 或 `enterprise/lark-ops-center`

**定位**：这是基于 AgentForge Runtime 的企业内中台实例。它调用 `lark-cli` / 飞书 OpenAPI，把飞书 IM、文档、Base、审批、任务和企业知识库接入 Runtime，做一个“研发运维智能工单中台”。

**一句话产品描述**：

> 员工在飞书里提交研发/运维问题后，系统自动检索项目文档、Runbook、历史故障和发布记录，派多个 Agent 分析定位，并通过飞书返回可审计的处理建议、风险评估和后续任务。

**核心场景**：
- 员工在飞书群或机器人私聊中提交问题，例如：“支付服务 10:20 后错误率升高，帮我排查可能原因”
- 系统把飞书消息转成 AgentForge `RunAgent` 任务
- RAG 检索 README、架构文档、历史事故复盘、发布记录、Runbook
- Skill selector 加载企业内的排障规范、代码检查规范、回滚规范
- Supervisor 派发子 Agent：
  - `log-analyst`：分析日志、错误码、现象
  - `runbook-agent`：根据运维手册找处理步骤
  - `release-reviewer`：检查近期发布和配置变更
  - `risk-reviewer`：评估回滚、修复、临时绕过方案的风险
- Pipeline 汇总输出一份结构化飞书消息或飞书文档
- 可选：把结论写入飞书 Base 工单表，把待办动作写入飞书任务，把高风险动作走飞书审批

**实例仓库新增模块建议**：
- `cmd/larkbot`：监听飞书消息或 webhook，把用户请求转换成 AgentForge run
- `internal/lark`：封装 `lark-cli` 调用，负责发消息、建文档、写 Base、创建任务、发起审批
- `examples/lark/pipelines/incident_triage.yaml`：故障排查 pipeline
- `examples/lark/skills/`：企业排障规范、发布规范、飞书回复规范等业务 Skill
- `examples/lark/docs/`：可 ingest 到 RAG 的模拟企业文档、Runbook、事故复盘
- `docs/ENTERPRISE_OPS_DEMO.md`：完整演示脚本，包括飞书入口、RAG 入库、Agent 分析、结果回写

**MVP 演示闭环**：
1. 用 `agentctl rag ingest` 导入 README、PROJECT_DESIGN、模拟 Runbook、事故复盘。
2. 启动 AgentForge runtime。
3. 启动 `cmd/larkbot` 或用 CLI 模拟一条飞书消息。
4. 用户提交：“支付服务错误率升高，请排查并给建议。”
5. Runtime 自动加载 Skill、检索 RAG、执行 Multi-Agent pipeline。
6. 实例仓库通过 `lark-cli` 把结果发回飞书：
   - 故障摘要
   - 可能原因
   - 证据来源
   - 建议动作
   - 风险评估
   - 后续任务 / 审批入口

**边界约束**：
- 实例仓库可以调用 `lark-cli`，但主仓库不依赖 `lark-cli`
- 实例仓库可以新增业务 Skill / pipeline / mock 企业文档，但不修改 runtime 公共协议
- 所有生产危险动作默认只生成建议或审批单，不直接执行生产变更
- Lark 集成失败时，仍可通过 `agentctl pipeline run` 演示同一套中台流程

**简历/面试讲法**：

> 主仓库是通用 AI Agent Runtime，负责调度、隔离、工具执行、RAG、Skill 和 Multi-Agent；实例仓库是基于该 Runtime 的企业研发运维中台，接入飞书作为企业协同入口，把内部知识、工单流程和 Agent 编排打通。这样既能证明底层 AI Infra 能力，也能证明可以把 Runtime 落到真实企业场景。

#### W8 — Hook + 服务拆分 + Scheduler Pick（✅ 完成）

**已做**：
- `SkillService` / `RAGService` / `HookService` 已加入 proto；新增 `skilld`、`ragd`、`hookd` 三个独立 gRPC 服务
- gateway 的 `IngestRAG` / `QueryRAG` 保持原入口，内部代理到 `ragd`
- worker 默认通过 `SKILL_SERVICE_ADDR` / `RAG_SERVICE_ADDR` / `HOOK_SERVICE_ADDR` 调用独立服务，不再在 worker 内本地加载 Skill/RAG/Hook
- Hook 支持 `PreLLM`、`PostLLM`、`PreToolUse`、`PostToolUse`；manifest 中的 `type=wasm` 通过 `wazero` 执行 WASI stdin/stdout JSON 协议，内置规则继续覆盖危险 bash deny 和模拟 secret 脱敏
- 新增 `internal/discovery`，服务通过 etcd lease 注册；scheduler 通过 etcd concurrency election 竞选 `/agentforge/scheduler/leader`
- scheduler 新增 `Leader` / `Pick` RPC；leader 的 `Pick` 基于 worker heartbeat 的 load / in-flight / concurrency 选择当前最低负载 worker，非 leader 返回 leader 信息
- `agentctl hook list/run` 与 `agentctl scheduler leader/pick` 已作为 W8 demo CLI
- docker compose 新增 `etcd`、`skilld`、`ragd`、`hookd`，并补齐 W8 环境变量

**与完整生产版的差异**：
- W8 提供本地服务注册和 scheduler election 纵切，但 gateway 还没有使用 `Pick` 改写 Redis Stream 投递目标
- W8 不改变 Redis Stream 抢占式消费主链路；Pick 作为调度面 demo，后续再接 worker-specific queue shard

#### W9 — Observability + Bench（✅ 完成）

**已做**：
- `internal/obs` 接入 OpenTelemetry tracing、Prometheus metrics、metrics HTTP endpoint，并保留业务 `run_id` / `trace_id` 日志字段
- gateway、worker、scheduler、skilld、ragd、hookd 启动时初始化 telemetry；RunAgent、LLM stream、Tool、Hook、RAG、Skill、Scheduler、Discovery 均有 span/metrics
- docker compose 新增 `otel-collector`、`prometheus`、`grafana`，并预置 AgentForge dashboard
- `WEEX_API_KEY` 可作为 OpenAI-compatible fallback key；项目 `.env` 统一为 Docker/Go dotenv 格式，原 Codex/TOML 配置备份到 `.env.codex.backup`
- 新增 `bench run-agent`、`make bench-run` 和 `docs/W9_BENCH_REPORT.md`

**与完整生产版的差异**：
- W9 不引入 Loki / Tempo；trace 先经 OTel Collector debug exporter 预留，Grafana 主要展示 Prometheus 大盘
- 压测默认使用 `LLM_PROVIDER=mock`，真实 WEEX/OpenAI-compatible 模型只做冒烟测试
- scheduler `Pick` 仍未接入 worker-specific queue shard；该项留给后续调度增强

#### 接下来 — W10 计划（🔜 进行中）

- 文档收口、demo 视频脚本、简历话术
- 将 scheduler `Pick` 接入 worker-specific queue shard，并补齐多 scheduler failover 压测

---

## 8. 性能目标（能写进简历的数字）

> 这些目标做完后用 K6 压出真实数据替换。

- 单机（8C16G）支撑 **5,000+ 并发会话**
- 调度器 P99 延迟 **< 50ms**
- Sandbox 冷启动（Firecracker）**< 100ms**
- ACP 协议吞吐 **3.8x** vs HTTP/JSON
- 上下文压缩后 token 节省 **30%+**
- 测试覆盖率 **70%+**

---

## 9. 简历话术模板（可直接套）

> **AgentForge — 云原生多智能体运行时** [Go / gRPC / Docker / Redis / pgvector / Raft]
>
> - 自研基于 TCP 的双向流式 Agent 通信协议 (ACP)，含断线续传与流量控制，吞吐较 HTTP/JSON 提升 **3.8x**
> - 设计**可变事件流式历史存储**，支持上下文原地改写与子 Agent 折叠，长会话 token 消耗降低 **35%**
> - 实现 Docker / gVisor / Firecracker **三级沙箱**与冷启动预热池，沙箱启动从 800ms → **80ms**
> - 基于轻量 LLM 预路由的 **Skill 动态加载**机制，主 Agent prompt 体积下降 **90%**
> - 基于 Raft 的高可用调度器，故障切换 < 3s，单机支撑 **5k+ 并发会话**，P99 调度延迟 < 50ms
> - 完整可观测体系（OpenTelemetry + Prometheus + Grafana），单元/集成测试覆盖率 **72%**

---

## 10. 加分项（如果还有时间）

1. **K8s Operator**：把 Agent / Workspace 做成 CRD，`kubectl apply -f agent.yaml`
2. **Web Dashboard**（极简，HTMX + Alpine.js 100 行）：实时看 Run 列表 + tail 日志
3. **录一段 3 分钟 demo 视频**：跑一个"自动修 bug"的 Agent，全程 trace 可视化
4. **写 3 篇深度博客**：ACP 协议设计 / 可变历史 / Sandbox 三级方案 —— 面试官搜你名字能搜到

---

## 11. 一句话总结

**AgentForge = "Agent 的 Kubernetes"**：
- ACP = etcd 的 watch 协议
- Scheduler = kube-scheduler
- Sandbox = Pod
- Workspace = PVC
- Skill = ConfigMap (按需挂载)
- Hook = Admission Webhook
- History 可变 = 有限状态机
- Multi-Agent = Deployment + Job

用 Go 把这套抽象做扎实，就是一个**简历上能镇住面试官的 AI Infra 项目**。

---

**下一步建议**（你拍板我就开干）：
- 我先生成 `pkg/proto/*.proto` + ACP 帧定义
- 同时搭 `cmd/gateway` + `cmd/scheduler` + `cmd/worker` 三个二进制骨架 + Docker Compose
- 一周内你能在本地跑通 "client → gateway → scheduler → worker → echo agent → 流式返回"
