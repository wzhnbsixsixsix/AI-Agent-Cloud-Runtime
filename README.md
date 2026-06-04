# AI-Agent-Cloud-Runtime

> 项目代号：**AgentForge** — 云原生多智能体运行时。  
> 详细设计见 [`PROJECT_DESIGN.md`](./PROJECT_DESIGN.md)。

当前进度：**W1 骨架已落地**。可在本地用 Docker Compose 一键启动 `gateway + scheduler + worker + redis`，通过 `agentctl run --prompt "..."` 流式收到 OpenAI 兼容 API 的逐 token 响应。

---

## 1. W1 架构

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

## 7. 当前 W1 已落地

- [x] Protobuf 定义 (`agent.proto` / `scheduler.proto`) + buf 生成
- [x] gRPC 双向流接入 (`AgentService.RunAgent`)
- [x] Redis Stream 任务队列 + Consumer Group + DLQ + 重试
- [x] Pub/Sub 事件回推
- [x] 可变历史（Append / Patch / Hide / Render）
- [x] OpenAI 兼容 SSE Provider + Mock Provider
- [x] worker 注册 / 心跳到 scheduler
- [x] CLI 客户端（cobra）
- [x] Docker Compose 一键起，distroless 运行时
- [x] 单测：history / llm / queue 共 ≥ 5 个

## 8. Roadmap（参考 PROJECT_DESIGN.md §7）

| 周 | 主题 |
|---|---|
| W2 | ACP 自研协议（帧编解码 + 双向流 + 断线续传 + benchmark） |
| W3 | Sandbox L1（Docker driver + 预热池 + 内置 tool） |
| W4 | LLM + 可变历史进阶（Fold / 子 Agent 折叠） |
| W5 | Skill 索引 + Selector |
| W6 | RAG（pgvector + reranker） |
| W7 | Multi-Agent 编排 + 上下文压缩 |
| W8 | WASM Hook + Raft scheduler + etcd 服务发现 |
| W9 | OTel + Grafana + K6 压测 |
| W10 | 文档、demo 视频、简历话术 |

---

## License

WIP（求职作品集项目，暂未指定 License）。
