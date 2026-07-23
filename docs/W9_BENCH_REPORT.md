# W9 Bench Report

## Environment

- Date:
- Machine:
- OS / Docker:
- Git commit:
- Worker replicas:
- Worker concurrency:
- LLM provider: `mock`
- RAG enabled:
- Hook enabled:
- Observability enabled:

## Commands

```bash
LLM_PROVIDER=mock HOOK_ENABLED=true RAG_ENABLED=false make up
make bench-run
```

## Results

| Scenario | Total | Concurrency | Throughput | P50 | P95 | P99 | Error Rate |
|---|---:|---:|---:|---:|---:|---:|---:|
| RunAgent mock |  |  |  |  |  |  |  |

## Prometheus Queries

```promql
sum by (status) (rate(agentforge_runs_total[1m]))
histogram_quantile(0.95, sum by (le, status) (rate(agentforge_run_duration_seconds_bucket[5m])))
sum(rate(agentforge_run_tokens_total[1m]))
histogram_quantile(0.95, sum by (le, tool, status) (rate(agentforge_tool_duration_seconds_bucket[5m])))
agentforge_scheduler_live_workers
```

## Grafana Evidence

- Dashboard URL: http://localhost:3000/d/agentforge-w9
- Screenshot:

## Notes

- W9 uses `LLM_PROVIDER=mock` for repeatable runtime benchmarks.
- Real GLM/OpenAI-compatible smoke tests are useful for integration confidence but should not be mixed with benchmark numbers.
