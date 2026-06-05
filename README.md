# AI-Agent-Cloud-Runtime

> 项目代号：**AgentForge** — 云原生多智能体运行时。  
> 详细设计见 [`PROJECT_DESIGN.md`](./PROJECT_DESIGN.md)。

当前进度：**W3 完成 — Sandbox L1（Docker driver + 预热池 + 5 个内置 tool）**。

- W1：可在本地用 Docker Compose 一键启动 `gateway + scheduler + worker + redis`，通过 `agentctl run --prompt "..."` 流式收到 OpenAI 兼容 API 的逐 token 响应。
- W2：gateway 同时监听 `:8080`(gRPC) 与 `:8090`(ACP)。`agentctl --proto acp` 可走自研协议；新增 `agentctl resume --run-id ...` 演示断线续传；`bin/bench` 工具一条命令打两条路径出对比数据。
- **W3**：worker 内置 Docker sandbox driver + 预热池 + bash/fs_read/fs_write/fs_list/http_fetch 5 个 tool；新增 gRPC `ListTools` / `ExecTool` 两个 RPC；`agentctl tool list` / `agentctl tool exec <name> --args '<json>'` 直接调用，留出 W4 接入 LLM function-calling 的钩子。

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
│   ├── queue/        # Redis Stream 消费组 + Pub/Sub
│   ├── scheduler/    # Scheduler interface + Redis 实现
│   └── storage/redis # 客户端工厂 + key 模板
├── pkg/proto/        # agent.proto / scheduler.proto（gen/ 由 buf 生成）
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

# 或者本地装 Go 后：
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
| `make build` | 本地编译四个二进制到 `bin/`（需 Go） |
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

## 10. Roadmap（参考 PROJECT_DESIGN.md §7）

| 周 | 主题 |
|---|---|
| W4 | LLM function-calling 闭环 + History Patch / Fold（复用 W3 tool registry） |
| W5 | Skill 索引 + Selector |
| W6 | RAG（pgvector + reranker） |
| W7 | Multi-Agent 编排 + 上下文压缩 |
| W8 | WASM Hook + Raft scheduler + etcd 服务发现 |
| W9 | OTel + Grafana + K6 压测 |
| W10 | 文档、demo 视频、简历话术 |

---

## License

WIP（求职作品集项目，暂未指定 License）。
