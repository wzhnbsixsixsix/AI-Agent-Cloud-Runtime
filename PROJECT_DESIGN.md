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
| **WASM (wasmtime-go)** | 轻量 Hook 脚本运行时 | 用户自定义 Hook 不用起容器 |
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
| PreToolUse / PostToolUse | tool 调用前后 | WASM (wasmtime) 跑用户脚本，10ms 内执行 |
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

| 周 | 阶段 | 交付（每周末必须能 demo） |
|---|---|---|
| W1 | 骨架 | Gateway + Scheduler + Worker 单体跑通，echo agent，Docker Compose 一键起 |
| W2 | ACP 协议 v1 | 帧编解码 + 双向流 + 断线续传，单测 + benchmark |
| W3 | Sandbox L1 | Docker driver + 预热池 + 隔离 + 5 个内置 tool |
| W4 | LLM + History | OpenAI/Claude provider + 流式 + 可变历史 + Patch/Fold |
| W5 | Skill | 索引 + Selector + 缓存 + 5 个内置 skill |
| W6 | RAG | pgvector + 混合检索 + reranker |
| W7 | Multi-Agent | Supervisor + Pipeline + 上下文压缩三级 |
| W8 | Hook + 微服务化 | WASM Hook + gRPC 拆分 + etcd 发现 + Raft |
| W9 | 可观测 + 压测 | OTel + Grafana 大盘 + K6 压测，产出报告 |
| W10 | 打磨 + 文档 | README、架构图、ADR、demo 视频、简历话术 |

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
