# AgentForge 最终验收清单

这份清单用于录 demo、发仓库、面试前自检。

## 1. 静态检查

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

## 2. Runtime Smoke

```bash
make up
./bin/agentctl run --prompt "用一句话介绍 AgentForge"
```

期望：流式输出以 `[DONE] run_id=... trace_id=...` 结束。

## 3. Hook Demo

```bash
./bin/agentctl hook list --addr localhost:8083

./bin/agentctl hook run --addr localhost:8083 \
  --event PreToolUse \
  --file examples/hooks/pretool_bash.json
```

期望：危险 bash 被拒绝，结果里能看到 `allowed=false` 或 deny 信息。

## 4. RAG Demo

```bash
./bin/agentctl rag ingest --path README.md --tenant default

./bin/agentctl rag query \
  --query "W9 可观测怎么工作" \
  --tenant default
```

期望：返回来自项目文档的 chunk。

## 5. Pipeline Demo

```bash
./bin/agentctl pipeline run --file examples/pipeline/readme-review.yaml
```

期望：输出有序 step 结果、run id 和成功状态。

## 6. Observability Demo

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

## 7. Benchmark Demo

```bash
LLM_PROVIDER=mock HOOK_ENABLED=true RAG_ENABLED=false make up
make bench-run
```

期望：`bench run-agent` 打印 throughput、p50、p95、p99 和失败数。

## 8. 文档检查

- README 顶部显示 W10 完成。
- `PROJECT_DESIGN.md` Roadmap 标记 W1-W10 完成。
- `STARTUP_GUIDE.md` 包含 W10 从零演示路线。
- `docs/FINAL_DELIVERY.md` 能链接到 demo、架构和面试材料。
- 文档没有把 gVisor、Firecracker、eBPF、CRIU、Loki、Tempo、worker-specific queue shard 写成已实现。

## 9. 最终边界声明

面试或视频中要明确：

- main 是通用 AI runtime，不是 Lark 业务仓库。
- 企业 Lark 中台作为 fork / 分支计划，不污染 runtime main。
- 性能数字以 `docs/W9_BENCH_REPORT.md` 的本机实测为准，不口头承诺未经验证的绝对数字。
