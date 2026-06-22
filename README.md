# AI-Agent-Cloud-Runtime

> 项目代号：**AgentForge** — 云原生多智能体运行时。  
> 详细设计见 [`PROJECT_DESIGN.md`](./PROJECT_DESIGN.md)。

当前进度：**W7 完成 — Multi-Agent + Context Compression 纵切**。

- W1：可在本地用 Docker Compose 一键启动 `gateway + scheduler + worker + redis`，通过 `agentctl run --prompt "..."` 流式收到 OpenAI 兼容 API 的逐 token 响应。
- W2：gateway 同时监听 `:8080`(gRPC) 与 `:8090`(ACP)。`agentctl --proto acp` 可走自研协议；新增 `agentctl resume --run-id ...` 演示断线续传；`bin/bench` 工具一条命令打两条路径出对比数据。
- **W3**：worker 内置 Docker sandbox driver + 预热池 + bash/fs_read/fs_write/fs_list/http_fetch 5 个 tool；新增 gRPC `ListTools` / `ExecTool` 两个 RPC；`agentctl tool list` / `agentctl tool exec <name> --args '<json>'` 直接调用，留出 W4 接入 LLM function-calling 的钩子。
- **W4**：worker 的 `agent.Runner` 已接入 OpenAI 兼容 function-calling；模型可在一次 `agentctl run` 中请求内置 tool，本地 sandbox 执行后把 `role=tool` 结果喂回模型继续生成；`internal/history` 新增 `Fold`，支持把历史区间折叠成一条 compacted 摘要。
- **W5**：新增 `internal/skill` 动态加载链路，worker 启动时扫描 `skills/**/SKILL.md`，按 prompt 规则选择 Top-K skill 并把完整内容注入本次 LLM system context；内置 5 个 skill 覆盖 sandbox 文件、bash、HTTP、Go 测试和项目说明。
- **W6**：新增 `internal/rag`，支持本地文本/Markdown/代码文件切片、确定性 hash embedding、Postgres + pgvector 存储、hybrid query、`agentctl rag ingest/query`，worker 可在 Run 前检索相关 chunk 并以 `<untrusted>` context 注入 LLM。
- **W7**：新增 `internal/orchestrator`，支持本地 Supervisor subagent、pipeline DAG demo 和 History 自动压缩；`dispatch_subagent` 可作为 LLM tool 暴露，`agentctl pipeline run --file ...` 可运行多 step 编排。

---

## 1. W2 架构（ACP / gRPC 双入口）

```
                     ┌────────────────────────────┐
   agentctl ─grpc──▶ │   gateway                  │── XADD ──▶ Redis Stream ──▶ worker
            ─acp───▶ │   :8080 grpc / :8090 acp   │                                │
       (--proto)     │                            │◀── PUBLISH events:{run_id} ────┘
                     │  ACP session 把每条 EVENT  │
                     │  同步写入 ZSet acp:events: │
                     │  {run_id} 用于断线续传     │
                     └────────────────────────────┘
```

ACP 协议规范见 [`pkg/acp/spec.md`](./pkg/acp/spec.md)。要点：

- 裸 TCP + 自定义帧（magic+ver+type+flags+seq(8B)+uvarint len+payload）
- 9 种帧类型：HELLO / HELLO_ACK / RUN / EVENT / PING / PONG / RESUME / CLOSE / ERROR
- 控制帧用 JSON，业务帧 RUN/EVENT 复用 W1 的 protobuf 定义
- 断线续传：服务端把每条 EVENT 落 ZSet（score=seq, TTL=1h），客户端 RESUME 带 last_seq 即可

W2 阶段控制面（`gateway↔scheduler`）保持 gRPC，只在外部入口（`client↔gateway`）做协议替换；这样 ACP 的优势 benchmark 才有公平对照。

---

## 2. W1 架构

```
   agentctl ──gRPC stream──▶ gateway :8080
                                │
                                │ XADD queue:agent_tasks
                                ▼
                            ┌────────┐
                            │ Redis  │◀── HSET/ZADD history:{run_id}
                            └────────┘
                                ▲ ▲
                       PUBLISH  │ │  XREADGROUP
                       events:  │ │
                       {run_id} │ │
                                │ ▼
                            ┌────────┐    HTTP SSE   ┌──────────────────┐
                            │ worker │ ────────────▶│ OpenAI 兼容 API │
                            └────────┘               └──────────────────┘
                                ▲
                                │ Register / Heartbeat
                                ▼
                            scheduler :8081/:8082
```

- **gateway**：gRPC 双向流入口，把任务写到 Redis Stream，订阅 `events:{run_id}` 把 worker 推回的事件透传给客户端。
- **scheduler**（W1）：仅做 worker 注册 / 心跳；W8 引入 Raft 后参与任务路由。
- **worker**：消费 Stream，调 LLM Provider 流式吐 token，写历史并 `PUBLISH` 事件。
- **agentctl**：cobra CLI，连 gateway 流式打印 token，终态打印 `[DONE] run_id=... trace_id=...`。

完整设计见 `PROJECT_DESIGN.md` 第 3、4 章。

---

## 2. 目录结构

```
AI-Agent-Cloud-Runtime/
├── cmd/
│   ├── gateway/      # gRPC 入口
│   ├── scheduler/    # worker 注册中心
│   ├── worker/       # 任务消费 + LLM 调用
│   └── agentctl/     # CLI 客户端
├── internal/
│   ├── agent/        # Run 状态机 + Runner
│   ├── config/       # env -> 三套 Config
│   ├── history/      # 可变历史（Redis Hash+ZSet）
│   ├── llm/          # Provider（OpenAI SSE / Mock / factory）
│   ├── obs/          # slog logger + trace_id
│   ├── orchestrator/ # W7 Supervisor / Pipeline / Compact
│   ├── queue/        # Redis Stream 消费组 + Pub/Sub
│   ├── rag/          # W6 chunking / embedding / pgvector retrieve
│   ├── scheduler/    # Scheduler interface + Redis 实现
│   ├── skill/        # W5 Skill 索引 + Selector + 缓存
│   └── storage/redis # 客户端工厂 + key 模板
├── pkg/proto/        # agent.proto / scheduler.proto（gen/ 由 buf 生成）
├── skills/           # 内置 SKILL.md
├── build/Dockerfile  # 多阶段（buf-gen → go-build → distroless）
├── deploy/           # docker-compose.yml + override 示例
├── Makefile
├── buf.yaml / buf.gen.yaml
├── .env.example
└── PROJECT_DESIGN.md
```

---

## 3. 环境要求

- **Docker Desktop**（含 docker compose v2）—— 跑服务必备
- **GNU Make + bash**（Windows 推荐 git-bash 或 WSL）
- 可选（仅本地编译/测试时需要）：
  - **Go 1.22+**
  - **buf** 或直接用 `make proto`（自动起 docker buf 镜像）
- 一个可用的 **OpenAI 兼容 API key**（OpenAI / DeepSeek / 通义 / Moonshot 都行）

---

## 4. 快速开始（W1 验收）

```bash
# 1) 准备 env
cp .env.example .env
#   编辑 .env，填入 OPENAI_API_KEY，必要时改 OPENAI_BASE_URL / OPENAI_MODEL

# 2) 一键起服（首次会下载镜像 + 编译）
make up                    # 等价于 docker compose up -d --build
make logs                  # 查看日志（Ctrl-C 退出，服务仍在）

# 3) 用 CLI 跑一次（容器内构建好的二进制也可以拷出来用，下面用 docker run 一次性跑）
docker compose -f deploy/docker-compose.yml run --rm \
  -e GATEWAY_DIAL_ADDR=gateway:8080 \
  --entrypoint /app \
  -v "$PWD":/work \
  --build-arg BIN=agentctl \
  worker /app run --prompt "用一句话介绍你自己"

# 或者用 Makefile 构建 CLI；本机没有 Go 时会自动使用 golang Docker 镜像
make proto && make build
GATEWAY_DIAL_ADDR=localhost:8080 ./bin/agentctl run --prompt "用一句话介绍你自己"
```

期望输出：

```
（流式逐字打印 LLM 回复 ...）
[DONE] run_id=01HV... trace_id=01HV... tokens=42
```

完成后：

```bash
make down   # 停服并清理 redis 数据
```

---

## 5. 常用命令

| 命令 | 说明 |
|------|------|
| `make help` | 列出全部 target |
| `make proto` | 用 docker buf 镜像生成 `pkg/proto/gen/*.pb.go` |
| `make build` | 编译所有二进制到 `bin/`；本机无 Go 时自动使用 golang Docker 镜像 |
| `make test` | 跑全部单测（`go test -race ./...`） |
| `make cover` | 输出测试覆盖率 |
| `make up` / `make down` | docker compose 起 / 停 |
| `make logs` | 跟随日志 |
| `docker compose -f deploy/docker-compose.yml up --scale worker=4 -d` | 横向扩展 worker |

---

## 6. 关键环境变量

完整列表见 `.env.example`，要点：

| 变量 | 默认 | 说明 |
|------|------|------|
| `LLM_PROVIDER` | `openai` | `openai` 或 `mock`（无 key 兜底） |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | 兼容任何 OpenAI 协议端点 |
| `OPENAI_API_KEY` | _(必填)_ | provider=openai 时必填 |
| `OPENAI_MODEL` | `gpt-4o-mini` | 默认模型 |
| `WORKER_CONCURRENCY` | `4` | 单 worker 进程内 consumer 数 |
| `WORKER_REPLICAS` | `1` | docker compose worker 副本数 |
| `REDIS_ADDR` | `redis:6379` | 容器内地址 |
| `SKILL_ENABLED` | `true` | 是否启用 W5 动态 Skill 加载 |
| `SKILL_ROOT` | `skills` | 本地 skill 根目录；docker compose 默认 `/skills` |
| `SKILL_TOP_K` | `3` | 单次 Run 最多注入几个 skill |
| `POSTGRES_DSN` | `postgres://...` | W6 RAG 的 Postgres/pgvector DSN |
| `RAG_ENABLED` | `false` | 是否启用 RAG RPC 与 worker 检索注入 |
| `RAG_TOP_K` | `5` | 每次 Run/query 最多检索几个 chunk |
| `MULTI_AGENT_ENABLED` | `false` | 是否向 LLM 暴露 `dispatch_subagent` |
| `CONTEXT_COMPACT_ENABLED` | `true` | 是否开启 History 自动压缩 |

---

## 7. 当前已落地

### W1
- [x] Protobuf 定义 (`agent.proto` / `scheduler.proto`) + buf 生成
- [x] gRPC 双向流接入 (`AgentService.RunAgent`)
- [x] Redis Stream 任务队列 + Consumer Group + DLQ + 重试
- [x] Pub/Sub 事件回推
- [x] 可变历史（Append / Patch / Hide / Render）
- [x] OpenAI 兼容 SSE Provider + Mock Provider
- [x] worker 注册 / 心跳到 scheduler
- [x] CLI 客户端（cobra）
- [x] Docker Compose 一键起，distroless 运行时

### W2 — ACP 自研协议
- [x] 协议规范 [`pkg/acp/spec.md`](./pkg/acp/spec.md) + 帧编解码（`pkg/acp`）
- [x] ACP server / session / event-cache（Redis ZSet）/ client（`internal/acp`）
- [x] gateway 同时监听 gRPC `:8080` 与 ACP `:8090`，gRPC 注册 `health.v1`
- [x] 断线续传：服务端缓存事件，客户端 `RESUME{last_seq}` 回放
- [x] `agentctl --proto acp|grpc` 切换 + `agentctl resume --run-id ... --last-seq ...`
- [x] `bin/bench rtt | throughput | connect` 一条命令打两条路径出对比数据
- [x] 单测：codec 5 例 + ACP server happy/ping/resume 3 例

### W3 — Sandbox L1
- [x] `internal/sandbox`：`Driver` 接口 + DockerDriver（预热池 N=4 + 异步补位）+ MemoryDriver（降级）
- [x] 容器隔离：`network=none` + `read-only rootfs` + `cap_drop ALL` + `no-new-privileges` + tmpfs + memory/cpu/pids cgroup + exec 硬超时
- [x] `internal/tool`：5 个内置 tool（bash / fs_read / fs_write / fs_list / http_fetch），均带 OpenAI/Anthropic 兼容 JSON Schema
- [x] 独立 Stream `queue:tool_tasks` + Pub/Sub `tool_results:{call_id}` 做请求-响应
- [x] gRPC 新增 `ListTools` / `ExecTool` RPC + `agentctl tool list` / `agentctl tool exec`
- [x] 单测：MemoryDriver 4 例 + tool 6 组；docker 集成测试通过 build tag `integration_docker` 隔离

### W4 — OpenAI Tool-Calling + History Fold
- [x] `internal/llm`：新增 tool-aware `Message` / `ToolDefinition` / `ToolCall` / `TokenEvent`，OpenAI SSE 支持 streamed `delta.tool_calls` 分片聚合
- [x] `internal/agent.Runner`： bounded function-calling loop，状态机补齐 `RUNNING → WAITING_TOOL → RUNNING`，默认最多 5 轮 tool 调用
- [x] worker 复用 W3 的 `tool.Registry` + `sandbox.Driver` 做本地 tool 执行，不绕回 gateway / Redis RPC
- [x] `internal/history.Store` 新增 `Fold(ctx, runID, fromID, toID, summary)`，把闭区间消息软删并追加一条 `compacted=true` 摘要
- [x] 单测覆盖 OpenAI tool_call 分片、请求体 tools 字段、History Fold、Runner 文本路径 / tool loop / loop cap

### W5 — Skill 索引 + Selector
- [x] `internal/skill`：`Indexer` 扫描 `skills/**/SKILL.md`，轻量解析 frontmatter 的 `name` / `description`，记录 `sha256` / path / 完整内容
- [x] `RuleSelector`：基于 prompt 与 skill name/description/content 的确定性关键词打分，稳定返回 Top-K
- [x] `CachedSelector`：按规范化 query hash 做 TTL + 容量限制的本地缓存
- [x] `agent.Runner`：每次 Run 可选注入一条动态 skill system message；无 skill、选择失败或禁用时自动退回 W4 行为
- [x] 内置 5 个 skill：`sandbox-files` / `bash-debug` / `http-fetch` / `go-test` / `agentforge-overview`
- [x] 单测覆盖 frontmatter、索引、重复名、缺目录、selector、cache hit、Runner skill 注入和 skill+tool loop

### W6 — RAG 纵切
- [x] `internal/rag`：chunking、确定性 hash embedder、keyword reranker、Postgres + pgvector store、RAG service
- [x] `AgentService` 新增 `IngestRAG` / `QueryRAG`；`agentctl rag ingest/query` 可直接验收入库与检索
- [x] docker compose 新增 `postgres` (`pgvector/pgvector:pg16`)；gateway/worker 在 `RAG_ENABLED=true` 时初始化 schema
- [x] `agent.Runner`：Run 前按 prompt 检索 RAG chunk，并把结果作为 `<untrusted>` system context 注入；失败/空结果自动退回 W5 行为
- [x] 单测覆盖 chunk、embedding、rerank、tenant filter、min-score 和 Runner RAG 注入；Postgres 集成测试用 `integration_pg` build tag 隔离

### W7 — Multi-Agent + Context Compression
- [x] `internal/orchestrator`：Supervisor 限制、本地 child run、pipeline DAG 解析/拓扑排序、context compact policy
- [x] `agent.Runner`：`MULTI_AGENT_ENABLED=true` 时暴露 `dispatch_subagent`，拦截后本地执行隔离 child history，并把结构化 JSON 结果作为 tool message 返回父 Agent
- [x] `AgentService.RunPipeline` + `agentctl pipeline run --file ...`：gateway 按 DAG 顺序复用现有 Redis agent queue 执行 step
- [x] `ContextCompactPolicy`：超过阈值后发布 `COMPACTING` 状态，调用 `History.Fold` 折叠旧消息并保留首尾上下文
- [x] 单测覆盖 supervisor 限制、pipeline 拓扑/校验/依赖注入、Runner subagent、Runner compaction

## 8. W2 demo 命令

```bash
# 起服（gateway 会同时暴露 :8080 与 :8090）
make up

# 1) 跑一次 ACP 链路
./bin/agentctl run --proto acp --prompt "用一句话介绍你自己"

# 2) 同时 benchmark ACP vs gRPC（小消息 RTT）
./bin/bench rtt --grpc localhost:8080 --acp localhost:8090 -n 5000

# 3) 吞吐对比
./bin/bench throughput --grpc localhost:8080 --acp localhost:8090 -n 50000 -c 64

# 4) 建连耗时对比
./bin/bench connect --grpc localhost:8080 --acp localhost:8090 -n 1000 -c 50

# 5) 断线续传演示：先跑一次记下 run_id，再用 resume 拉取缓存
./bin/agentctl run --proto acp --prompt "讲个长故事..."     # 输出有 run_id=01HX...
./bin/agentctl resume --run-id 01HX... --last-seq 0
```

预期：RTT / Throughput 两个场景下 ACP 比 gRPC 快 ~3x（ACP 走单连接裸 TCP，无 HTTP/2 帧、HEADERS 压缩与流量调度开销）。

## 9. W3 demo — Sandbox + Tool

### 架构

```
   agentctl tool exec ──gRPC──▶ gateway ──XADD queue:tool_tasks──▶ Redis Stream
                                  ▲                                     │
                                  │ Subscribe tool_results:{call_id}    │
                                  │                                     ▼
                                  └─PUBLISH─── worker (tool consumer) ──┐
                                                       │                │
                                                       ▼                │
                                       ┌────────────── Sandbox.Driver ──┘
                                       │ DockerDriver: 预热池 N=4
                                       │   pop slot ─┬─ bind /tmp/agentforge/<id>/<run_id>
                                       │             │   → /workspace/runs/<run_id>
                                       │             ├─ ContainerExec(bash/fs/...)
                                       │             └─ release: force rm + 异步 spawn 新 slot
                                       └ MemoryDriver（无 docker 时降级，仅工程联调）
```

### 隔离矩阵（DockerDriver）

| 项 | 值 |
|---|---|
| 镜像 | `alpine:3.19`（启动后 `sleep infinity`） |
| network | `none`（容器侧无任何 NIC） |
| rootfs | `read-only` + `/tmp` tmpfs |
| caps | `cap_drop ALL` + `no-new-privileges` |
| memory | 默认 256 MiB |
| cpu | quota 50000 / period 100000（≈ 0.5 core） |
| pids | 256 |
| 单次 exec 时长 | 默认 60s |
| workspace | 宿主 `/tmp/agentforge/<slot>/runs/<run_id>` ⇄ 容器 `/workspace/runs/<run_id>` |

> `http_fetch` 故意不走 sandbox（容器无网络），改在 worker 主进程跑，靠 `TOOL_HTTP_ALLOW_LIST` + `TOOL_HTTP_MAX_BYTES` 兜底。

### 内置 tool

| 名字 | 用途 | 参数（Schema 见 `agentctl tool list --schema`） |
|------|------|----|
| `bash` | `sh -c <command>` | `{"command": "..."}` |
| `fs_read` | 读文件（head -c 截断） | `{"path": "...", "max_bytes": 65536}` |
| `fs_write` | 创建/覆盖/追加 | `{"path": "...", "content": "...", "append": false}` |
| `fs_list` | `ls -la` 列目录 | `{"path": "."}` |
| `http_fetch` | host 上发 HTTP GET | `{"url": "https://example.com/", "max_bytes": 1048576}` |

`fs_*` 全部走 `safePath` 校验，禁止 `../` 越出 workspace。

### demo 命令

```bash
# 0) 准备：把 sandbox 相关 env 加到 .env（默认值已经够 demo）
cp .env.example .env
#    若想试 http_fetch：在 .env 里补 TOOL_HTTP_ALLOW_LIST=httpbin.org

# 1) 起服（首次会拉 alpine:3.19）
make up

# 2) 列出全部 tool
./bin/agentctl tool list
./bin/agentctl tool list --schema     # 含 JSON Schema

# 3) 在 sandbox 里跑 bash
./bin/agentctl tool exec bash --args '{"command":"id && uname -a && ls -la /workspace"}'

# 4) 写文件 → 读回来
./bin/agentctl tool exec fs_write \
  --args '{"path":"hello.txt","content":"hi from sandbox\n"}'
./bin/agentctl tool exec fs_read --args '{"path":"hello.txt"}'

# 5) 验证网络隔离（应失败：容器内无网络）
./bin/agentctl tool exec bash --args '{"command":"wget -qO- https://example.com || echo BLOCKED"}'

# 6) 验证 read-only rootfs（应失败）
./bin/agentctl tool exec bash --args '{"command":"touch /etc/x 2>&1 | head -1"}'

# 7) host 侧 http_fetch（受 TOOL_HTTP_ALLOW_LIST 限制）
./bin/agentctl tool exec http_fetch \
  --args '{"url":"https://httpbin.org/get"}'
```

预期：第 5 / 6 步分别打印 `BLOCKED` 与 `Read-only file system`，证明 sandbox 隔离生效。

### 关键环境变量（W3）

| 变量 | 默认 | 说明 |
|------|------|------|
| `SANDBOX_DRIVER` | `docker` | `docker` / `memory` / `disabled` |
| `SANDBOX_IMAGE` | `alpine:3.19` | sandbox 容器镜像 |
| `SANDBOX_POOL_SIZE` | `4` | 预热池常驻 idle 容器数 |
| `SANDBOX_WORKSPACE_ROOT` | `/tmp/agentforge` | worker 容器内的 workspace 根 |
| `SANDBOX_WORKSPACE_HOST` | `/tmp/agentforge` | 宿主上对应路径（docker driver bind 源） |
| `SANDBOX_MEMORY_MB` | `256` | 容器内存上限 |
| `SANDBOX_CPU_QUOTA_US` | `50000` | cgroup CPU quota |
| `SANDBOX_PIDS_LIMIT` | `256` | pids cgroup 上限 |
| `SANDBOX_EXEC_HARD` | `60s` | 单次 exec 硬超时 |
| `TOOL_CONCURRENCY` | `4` | tool consumer 并发 |
| `TOOL_HTTP_MAX_BYTES` | `1048576` | http_fetch body 截断 |
| `TOOL_HTTP_ALLOW_LIST` | _空_ | 逗号分隔的 host 白名单；空则禁用 |
| `GATEWAY_TOOL_CALL_TIMEOUT` | `60s` | gateway 等 worker 结果的超时 |

## 10. W4 demo — Agent 自动调用 Tool

W4 不新增 RPC；仍然使用 `agentctl run`。区别是 worker 会把 W3 的 tool schema 注入 OpenAI 兼容 `tools` 字段，模型返回 `tool_calls` 后由 worker 本地执行，再把 `role=tool` 结果喂回模型继续生成。

```bash
# 1) 起服，确保 .env 使用 OpenAI 兼容模型且 sandbox 开启
make up

# 2) 让模型自己决定调用 fs_write / fs_read / bash
./bin/agentctl run --prompt \
  "在工作目录创建 hello.txt，内容为 AgentForge W4，然后读回文件内容并用一句话总结。"

# 3) 如需防止模型循环调用工具，可调整最大 tool 轮数
AGENT_TOOL_MAX_STEPS=3 make up
```

期望：CLI 仍然流式输出最终回答；worker 日志中能看到 `WAITING_TOOL` 状态和具体 tool 调用。若 `SANDBOX_DRIVER=disabled` 或 Docker 不可用，worker 不会把 tools 暴露给 LLM，`agentctl run` 会自动退回 W1/W3 的纯文本路径。

### 关键环境变量（W4）

| 变量 | 默认 | 说明 |
|------|------|------|
| `AGENT_TOOL_MAX_STEPS` | `5` | 单次 Run 中模型/tool 循环的最大轮数，超出后返回 `tool_loop_limit` |

## 11. W5 demo — Skill 动态加载

W5 不新增 RPC；仍然使用 `agentctl run`。区别是 worker 启动时会扫描 `skills/**/SKILL.md`，每次 Run 根据 prompt 选择少量相关 skill，把完整 `SKILL.md` 内容作为额外 system message 注入 LLM 请求。

```bash
# 1) 本地构建后，用 mock provider 验证 skill 选择链路不依赖外部 LLM
LLM_PROVIDER=mock SKILL_ENABLED=true make up

# 2) 触发 sandbox-files skill
./bin/agentctl run --prompt "帮我列出 sandbox 文件工具怎么用"

# 3) 触发 go-test skill
./bin/agentctl run --prompt "这次 Go 代码修改应该怎么跑测试"
```

期望：CLI 仍然按原方式流式输出；worker 日志中能看到 `skill selector loaded` 和每次 Run 的 `skills loaded`。若 `SKILL_ROOT` 不存在、`SKILL_ENABLED=false` 或没有命中，worker 自动退回 W4 的 system prompt + history + tools 行为。

### 关键环境变量（W5）

| 变量 | 默认 | 说明 |
|------|------|------|
| `SKILL_ENABLED` | `true` | 是否启用动态 Skill 加载 |
| `SKILL_ROOT` | `skills` | 本地 skill 根目录；docker compose 默认 `/skills` |
| `SKILL_TOP_K` | `3` | 每次 Run 最多注入几个 skill |
| `SKILL_CACHE_TTL` | `10m` | selector 结果缓存 TTL |
| `SKILL_CACHE_SIZE` | `1024` | selector 结果缓存最大条目数 |

## 12. W6 demo — RAG 文档检索

W6 新增两个 gRPC RPC 和一个 CLI 子命令；`RunAgent` 和 ACP 不变。开启 RAG 后，worker 会在每次 Run 前根据 prompt 检索相关 chunk，并将结果包在 `<untrusted>` 中注入 LLM。

```bash
# 1) 开启 RAG 后起服
RAG_ENABLED=true make up

# 2) 本地构建 CLI 后，把 README 写入 RAG store
make build
./bin/agentctl rag ingest --path README.md --tenant default

# 3) 单独查询 RAG
./bin/agentctl rag query --query "W5 skill selector 怎么工作" --tenant default --top-k 5

# 4) 正常 run；worker 会自动检索并注入 RAG context
./bin/agentctl run --prompt "根据项目文档解释 W5 skill selector"
```

当前 W6 限制：默认 embedder 是确定性 hash embedding，便于无外部 key demo；PDF/docx、tree-sitter 代码解析和外部 bge reranker 留到后续增强。

### 关键环境变量（W6）

| 变量 | 默认 | 说明 |
|------|------|------|
| `POSTGRES_DSN` | `postgres://agentforge:agentforge@postgres:5432/agentforge?sslmode=disable` | RAG store DSN |
| `RAG_ENABLED` | `false` | 是否启用 RAG |
| `RAG_EMBED_DIM` | `384` | hash embedding 维度，同时决定 pgvector schema |
| `RAG_TOP_K` | `5` | 默认返回/注入 chunk 数 |
| `RAG_TENANT_ID` | `default` | worker Run 检索使用的 tenant |
| `RAG_MIN_SCORE` | `0` | 最低检索分数 |

## 13. W7 demo — Multi-Agent + 压缩

### Supervisor subagent

```bash
MULTI_AGENT_ENABLED=true make up
./bin/agentctl run --prompt "派一个子 Agent 总结 README，再用一句话给我结论"
```

期望：模型可调用 `dispatch_subagent`；worker 本地创建 child run，child 使用独立 history，父 run 只收到结构化 tool result。

### Pipeline DAG

```bash
./bin/agentctl pipeline run --file examples/pipeline/readme-review.yaml
```

`readme-review.yaml` 包含 `summarize -> critique -> final` 三个 step，gateway 会按拓扑顺序投递到现有 worker 执行。

### Context compression

```bash
CONTEXT_COMPACT_ENABLED=true CONTEXT_COMPACT_MAX_CHARS=1200 make up
```

长历史超过阈值后，worker 会发布 `COMPACTING` 状态并用 `History.Fold` 生成 `compacted=true` 摘要消息。

### 关键环境变量（W7）

| 变量 | 默认 | 说明 |
|------|------|------|
| `MULTI_AGENT_ENABLED` | `false` | 是否暴露 `dispatch_subagent` tool |
| `SUBAGENT_MAX_DEPTH` | `2` | subagent 最大递归深度 |
| `SUBAGENT_MAX_CHILDREN` | `4` | 单 parent run 最多 child 数 |
| `SUBAGENT_TIMEOUT` | `2m` | 单个 child run 超时 |
| `CONTEXT_COMPACT_ENABLED` | `true` | 是否启用自动压缩 |
| `CONTEXT_COMPACT_MAX_CHARS` | `24000` | 触发压缩的可见历史字符阈值 |
| `CONTEXT_COMPACT_KEEP_HEAD` | `4` | 压缩时保留开头消息数 |
| `CONTEXT_COMPACT_KEEP_TAIL` | `8` | 压缩时保留末尾消息数 |

## 14. W8 demo — Hook + 服务拆分 + Scheduler Pick

W8 将 Skill/RAG/Hook 拆成独立 gRPC 服务：`skilld`、`ragd`、`hookd`。Gateway 的 RAG RPC 保持不变但代理到 `ragd`；worker 通过服务地址调用 Skill/RAG/Hook。Scheduler 新增 `Leader` / `Pick` RPC，用于展示 Raft-backed 调度面的接口。

```bash
# 启动服务拆分版 runtime
HOOK_ENABLED=true RAG_ENABLED=true make up

# 查看 hookd 已加载 hooks
./bin/agentctl hook list --addr localhost:8083

# 列表里会包含 rule hook 和 wazero WASI hook；示例 wasm 源码在 hooks/wasm_enterprise_safety.go
# 重新生成示例 wasm：
docker run --rm -v "$PWD:/src" -w /src tinygo/tinygo:0.33.0 \
  tinygo build -target=wasi -tags tinygo_wasm_hook \
  -o hooks/wasm_enterprise_safety.wasm hooks/wasm_enterprise_safety.go

# 直接执行 PreToolUse hook，危险 bash 会被拒绝
./bin/agentctl hook run --addr localhost:8083 \
  --event PreToolUse \
  --file examples/hooks/pretool_bash.json

# 查询 scheduler leader
./bin/agentctl scheduler leader --addr localhost:8081

# 选择当前最低负载 worker
./bin/agentctl scheduler pick --addr localhost:8081 --run-id demo
```

当前 W8 说明：服务拆分、Hook gRPC、wazero WASI hook、PreLLM/PreToolUse/PostToolUse 行为、etcd lease 服务注册、scheduler Raft-backed election、Pick/Leader 已落地。W8 仍不改变 Redis Stream 抢占式消费主链路；`Pick` 先作为可 demo 的调度面，后续再接 worker-specific queue shard。

## 15. W9 demo — Observability + Bench

W9 接入 OpenTelemetry、Prometheus 和 Grafana。每个服务都会暴露 `/metrics`，Prometheus 默认抓取 gateway、worker、scheduler、skilld、ragd、hookd，Grafana 会自动加载 AgentForge dashboard。

```bash
# 项目 .env 使用 Docker/Go dotenv 格式；原 Codex/TOML 配置已备份到 .env.codex.backup
# WEEX_API_KEY 可作为 OPENAI_API_KEY 的 fallback
WEEX_API_KEY=...
LLM_PROVIDER=openai
OPENAI_BASE_URL=<weex-compatible-url>
OPENAI_MODEL=<model>

# 可复现压测默认使用 mock LLM
LLM_PROVIDER=mock HOOK_ENABLED=true RAG_ENABLED=false make up

# Grafana: http://localhost:3000  默认 admin/admin
# Prometheus: http://localhost:9090

# 运行 RunAgent mock 压测
make bench-run

# 真实 WEEX/OpenAI-compatible 冒烟测试
LLM_PROVIDER=openai ./bin/agentctl run --prompt "用一句话介绍 AgentForge"
```

常用 PromQL：

```promql
sum by (status) (rate(agentforge_runs_total[1m]))
histogram_quantile(0.95, sum by (le, status) (rate(agentforge_run_duration_seconds_bucket[5m])))
sum(rate(agentforge_run_tokens_total[1m]))
```

## 16. Roadmap（参考 PROJECT_DESIGN.md §7）

| 周 | 主题 |
|---|---|
| W4 | ✅ LLM function-calling 闭环 + History Patch / Fold（复用 W3 tool registry） |
| W5 | ✅ Skill 索引 + Selector |
| W6 | ✅ RAG（pgvector + reranker） |
| W7 | ✅ Multi-Agent 编排 + 上下文压缩 |
| W8 | ✅ Hook 服务 + Skill/RAG/Hook 服务拆分 + Scheduler Pick/Leader |
| W9 | ✅ OTel + Prometheus + Grafana + mock RunAgent 压测 |
| W10 | 文档、demo 视频、简历话术 |

---

## License

WIP（求职作品集项目，暂未指定 License）。
