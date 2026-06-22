SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

MODULE     := github.com/wzhnbsixsixsix/agentforge
BIN_DIR    := bin
BINARIES   := gateway scheduler worker skilld ragd hookd agentctl bench
GOFLAGS    := -trimpath
LDFLAGS    := -s -w
GO_CACHE_DIR ?= $(CURDIR)/.cache/go-build
GO_MOD_CACHE ?= $(CURDIR)/.cache/go-mod
GO_IMAGE  ?= golang:1.22-alpine

# Buf 优先使用本地安装；未安装时回退 Docker，避免强依赖本地 protoc。
BUF_IMAGE  := bufbuild/buf:1.45.0
DOCKER_RUN := docker run --rm -v "$(CURDIR)":/workspace -w /workspace
BUF        := $(shell command -v buf 2>/dev/null)
BUF_CACHE_DIR ?= $(CURDIR)/.cache/buf
TOOLS_BIN  := $(CURDIR)/bin/tools
PROTOC_GEN_GO_PATH := $(shell command -v protoc-gen-go 2>/dev/null)
PROTOC_GEN_GO_GRPC_PATH := $(shell command -v protoc-gen-go-grpc 2>/dev/null)
PROTOC_GEN_GO := $(if $(PROTOC_GEN_GO_PATH),$(PROTOC_GEN_GO_PATH),$(TOOLS_BIN)/protoc-gen-go)
PROTOC_GEN_GO_GRPC := $(if $(PROTOC_GEN_GO_GRPC_PATH),$(PROTOC_GEN_GO_GRPC_PATH),$(TOOLS_BIN)/protoc-gen-go-grpc)
LOCAL_GO := $(shell command -v go 2>/dev/null)

ifeq ($(LOCAL_GO),)
GO_RUN      := $(DOCKER_RUN) -e GOCACHE=/workspace/.cache/go-build -e GOMODCACHE=/workspace/.cache/go-mod $(GO_IMAGE)
GO          := $(GO_RUN) go
GO_INSTALL  := $(DOCKER_RUN) -e GOCACHE=/workspace/.cache/go-build -e GOMODCACHE=/workspace/.cache/go-mod -e GOBIN=/workspace/bin/tools $(GO_IMAGE) go install
GO_BUILD    := $(DOCKER_RUN) -e CGO_ENABLED=0 -e GOCACHE=/workspace/.cache/go-build -e GOMODCACHE=/workspace/.cache/go-mod $(GO_IMAGE) go build
GO_TEST     := $(GO_RUN) go test
GO_TOOL     := $(GO_RUN) go tool
GO_VET      := $(GO_RUN) go vet
else
GO_ENV      := GOCACHE="$(GO_CACHE_DIR)" GOMODCACHE="$(GO_MOD_CACHE)"
GO          := $(GO_ENV) go
GO_INSTALL  := $(GO_ENV) GOBIN="$(TOOLS_BIN)" go install
GO_BUILD    := CGO_ENABLED=0 $(GO_ENV) go build
GO_TEST     := $(GO_ENV) go test
GO_TOOL     := $(GO_ENV) go tool
GO_VET      := $(GO_ENV) go vet
endif

.PHONY: help
help: ## 列出可用命令
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n",$$1,$$2}'

.PHONY: tools
tools: $(PROTOC_GEN_GO) $(PROTOC_GEN_GO_GRPC) ## 安装 Go 侧代码生成工具到 bin/tools

$(PROTOC_GEN_GO):
	@mkdir -p $(TOOLS_BIN)
	@$(GO_INSTALL) google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2

$(PROTOC_GEN_GO_GRPC):
	@mkdir -p $(TOOLS_BIN)
	@$(GO_INSTALL) google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

.PHONY: proto
proto: ## 生成 protobuf Go 代码（优先本地 buf，缺失时回退 Docker）
	@if [ -n "$(BUF)" ]; then \
		$(MAKE) --no-print-directory tools; \
		PATH="$(TOOLS_BIN):$$PATH" BUF_CACHE_DIR="$(BUF_CACHE_DIR)" buf generate; \
	else \
		$(DOCKER_RUN) $(BUF_IMAGE) generate; \
	fi

.PHONY: proto-local
proto-local: ## 本地已装 buf 时使用
	@$(MAKE) --no-print-directory tools
	@PATH="$(TOOLS_BIN):$$PATH" BUF_CACHE_DIR="$(BUF_CACHE_DIR)" buf generate

.PHONY: tidy
tidy: ## go mod tidy
	@$(GO) mod tidy

.PHONY: build
build: $(BINARIES:%=build-%) ## 构建全部二进制

build-%:
	@echo ">> building $*"
	@mkdir -p $(BIN_DIR)
	@$(GO_BUILD) $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/$* ./cmd/$*

.PHONY: test
test: ## 跑全部单测
	@$(GO_TEST) -race -count=1 ./...

.PHONY: cover
cover: ## 输出覆盖率
	@$(GO_TEST) -race -count=1 -coverprofile=coverage.txt ./...
	@$(GO_TOOL) cover -func=coverage.txt | tail -n 1

.PHONY: lint
lint: ## go vet + buf lint
	@$(GO_VET) ./...
	@if [ -n "$(BUF)" ]; then \
		BUF_CACHE_DIR="$(BUF_CACHE_DIR)" buf lint || true; \
	else \
		$(DOCKER_RUN) $(BUF_IMAGE) lint || true; \
	fi

.PHONY: run
run: ## docker compose up（前台）
	@docker compose --env-file .env -f deploy/docker-compose.yml up --build

.PHONY: up
up: ## docker compose up -d
	@docker compose --env-file .env -f deploy/docker-compose.yml up -d --build

.PHONY: down
down: ## docker compose down
	@docker compose --env-file .env -f deploy/docker-compose.yml down -v

.PHONY: logs
logs: ## docker compose logs -f
	@docker compose --env-file .env -f deploy/docker-compose.yml logs -f

.PHONY: obs-config
obs-config: ## 校验 docker compose / Prometheus / Grafana 配置
	@docker compose --env-file .env -f deploy/docker-compose.yml config >/dev/null
	@echo "observability config ok"

.PHONY: bench-run
bench-run: build-bench ## W9: mock LLM RunAgent 压测
	@LLM_PROVIDER=mock GATEWAY_DIAL_ADDR=$${GATEWAY_DIAL_ADDR:-localhost:8080} \
		$(BIN_DIR)/bench run-agent --grpc $${GATEWAY_DIAL_ADDR:-localhost:8080} \
		--num $${BENCH_TOTAL:-100} --concurrency $${BENCH_CONCURRENCY:-8} \
		--prompt "$${BENCH_PROMPT:-用一句话介绍 AgentForge}"

.PHONY: bench-report
bench-report: ## 打印 W9 压测报告模板位置
	@echo "Fill docs/W9_BENCH_REPORT.md after running: make bench-run"

.PHONY: final-check
final-check: ## W10: 最终交付检查（proto + test + build + obs-config + diff）
	@$(MAKE) --no-print-directory proto
	@$(GO_TEST) ./...
	@$(GO_BUILD) ./cmd/...
	@$(MAKE) --no-print-directory obs-config
	@git diff --check

.PHONY: clean
clean: ## 清理构建产物
	@rm -rf $(BIN_DIR) coverage.txt
