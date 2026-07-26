# AgentForge 最终验收清单

这份清单用于录 demo、发仓库、面试前自检。

## 1. Dashboard 端到端验收

按 [`STARTUP_GUIDE.md`](../STARTUP_GUIDE.md) 启动全栈，打开 `http://localhost:5173`，依次验证：

- Control Plane 健康状态正常。
- 创建 Agent 后状态进入 `running`。
- 在 Agent 详情页启动 Run，并持续收到 SSE 事件。
- 刷新页面后仍能看到当前 Run 和历史事件。
- 工作区目录可展开，文本文件可只读预览。
- 停止 Agent 后状态正确更新。

期望：浏览器只访问 Nginx/Control Plane，不直接访问 gRPC、Redis 或 Docker socket；同一 Agent 同时只运行一个活跃 Run。

## 2. 静态检查

```bash
make final-check
```

等价于：

```bash
make proto
go test ./...
go build ./cmd/...
make obs-config
git diff --check
```

期望：所有命令退出码为 0。

## 3. Runtime Smoke

```bash
make up
./bin/agentctl run --prompt "用一句话介绍 AgentForge"
```

期望：流式输出以 `[DONE] run_id=... trace_id=...` 结束。

## 4. Hook Demo

```bash
./bin/agentctl hook list --addr localhost:8083

./bin/agentctl hook run --addr localhost:8083 \
  --event PreToolUse \
  --file examples/hooks/pretool_bash.json
```

期望：危险 bash 被拒绝，结果里能看到 `allowed=false` 或 deny 信息。

## 5. RAG Demo

```bash
./bin/agentctl rag ingest --path README.md --tenant default

./bin/agentctl rag query \
  --query "W9 可观测怎么工作" \
  --tenant default
```

期望：返回来自项目文档的 chunk。

## 6. Pipeline Demo

```bash
./bin/agentctl pipeline run --file examples/pipeline/readme-review.yaml
```

期望：输出有序 step 结果、run id 和成功状态。

## 7. Observability Demo

```bash
make obs-config
```

打开：

- Grafana: `http://localhost:3000`
- Prometheus: `http://localhost:9090`

常用 PromQL：

```promql
sum by (status) (rate(agentforge_runs_total[1m]))
histogram_quantile(0.95, sum by (le, status) (rate(agentforge_run_duration_seconds_bucket[5m])))
sum(rate(agentforge_run_tokens_total[1m]))
```

期望：至少跑过一次 Run 后，Grafana dashboard 有数据，Prometheus 能查到 AgentForge metrics。

## 8. Benchmark Demo

```bash
LLM_PROVIDER=mock HOOK_ENABLED=true RAG_ENABLED=false make up
make bench-run
```

期望：`bench run-agent` 打印 throughput、p50、p95、p99 和失败数。

## 9. 文档检查

- `README.md` 只承担项目入口和能力摘要。
- `STARTUP_GUIDE.md` 只承担 Dashboard/Compose 启动。
- `docs/CLI_RUNTIME_GUIDE.md` 完整保留 W1–W10 CLI 演示路线。
- `docs/CONTAINER_NETWORKING.md` 说明当前容器寻址和隔离边界。
- `PROJECT_DESIGN.md` 明确 Control Plane 已实现、Agent-to-Agent ACP Collaboration 仍为规划。
- `docs/FINAL_DELIVERY.md` 能链接到文档索引、演示、架构和面试材料。
- 文档没有把 gVisor、Firecracker、eBPF、CRIU、Loki、Tempo、worker-specific queue shard 写成已实现。

## 10. 最终边界声明

面试或视频中要明确：

- main 是通用 AI runtime，不是 Lark 业务仓库。
- 企业 Lark 中台作为 fork / 分支计划，不污染 runtime main。
- 当前 ACP 是 Gateway 的客户端流式协议；Agent-to-Agent Task/Result 协作网关尚未实现。
- 性能数字以 `docs/W9_BENCH_REPORT.md` 的本机实测为准，不口头承诺未经验证的绝对数字。

W1–W10 的详细命令集中在 [`CLI_RUNTIME_GUIDE.md`](./CLI_RUNTIME_GUIDE.md)，本清单不重复维护启动教程。
