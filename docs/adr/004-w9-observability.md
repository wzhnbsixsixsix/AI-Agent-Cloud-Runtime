# ADR 004：W9 选择 OTel + Prometheus + Grafana

## 状态

Accepted.

## 背景

Agent Runtime 需要能看见运行状态：Run 成功率、耗时、token、tool 错误、hook 延迟、scheduler 状态、worker capacity。完整日志和 trace UI 也很有价值，但一次性引入 Loki、Tempo 会增加 W10 交付复杂度。

## 决策

W9 交付：

- OpenTelemetry tracing helper 和 OTLP export
- 每个服务暴露 Prometheus metrics
- Grafana dashboard provisioning
- OTel Collector，先使用 debug trace exporter

Loki 和 Tempo 暂缓。

## 影响

- W10 demo 可以低成本展示真实 dashboard。
- trace 数据已经发出，后续可接入 Tempo。
- 文档中要明确：W10 的 Grafana 主要展示 Prometheus metrics，不声称已经有完整 Loki/Tempo 观测栈。
