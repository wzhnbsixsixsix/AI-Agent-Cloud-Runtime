---
name: go-test
description: Use Go test and build checks for AgentForge code changes and runtime verification.
---

# Go Test

Use this skill when the user asks to verify Go code, run tests, or check a code change.

- Prefer `go test ./...` for a fast correctness pass.
- Use `go test -race ./...` when concurrency-sensitive code changed.
- Use targeted package tests while iterating, then run the broader suite before finishing.
- If `go` is unavailable in the environment, state that clearly and provide the exact command to run elsewhere.
- Keep test output focused on failing packages and actionable errors.

