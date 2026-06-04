package redisstore

import "fmt"

// Keys 集中所有 Redis key 模板，避免散落字符串。
var Keys = struct {
	HistoryMsgs   func(runID string) string
	HistoryOrder  func(runID string) string
	QueueTasks    string
	QueueTasksDLQ string
	EventsTopic   func(runID string) string
	WorkerKey     func(workerID string) string
	WorkerSet     string
}{
	HistoryMsgs:   func(rid string) string { return fmt.Sprintf("history:%s:msgs", rid) },
	HistoryOrder:  func(rid string) string { return fmt.Sprintf("history:%s:order", rid) },
	QueueTasks:    "queue:agent_tasks",
	QueueTasksDLQ: "queue:agent_tasks:dlq",
	EventsTopic:   func(rid string) string { return fmt.Sprintf("events:%s", rid) },
	WorkerKey:     func(wid string) string { return fmt.Sprintf("worker:%s", wid) },
	WorkerSet:     "workers:active",
}
