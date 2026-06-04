SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

MODULE     := github.com/wzhnbsixsixsix/agentforge
BIN_DIR    := bin
BINARIES   := gateway scheduler worker agentctl bench
GOFLAGS    := -trimpath
LDFLAGS    := -s -w

# 通过 Docker 跑 buf，避免本地装 buf/protoc。
BUF_IMAGE  := bufbuild/buf:1.45.0
DOCKER_RUN := docker run --rm -v "$(CURDIR)":/workspace -w /workspace

.PHONY: help
help: ## 列出可用命令
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n",$$1,$$2}'

.PHONY: tools
tools: ## 安装 Go 侧工具（protoc-gen-go/protoc-gen-go-grpc，可选，buf 远程插件已自动处理）
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

.PHONY: proto
proto: ## 通过 docker 跑 buf generate
	@$(DOCKER_RUN) $(BUF_IMAGE) generate

.PHONY: proto-local
proto-local: ## 本地已装 buf 时使用
	@buf generate

.PHONY: tidy
tidy: ## go mod tidy
	@go mod tidy

.PHONY: build
build: $(BINARIES:%=build-%) ## 构建全部二进制

build-%:
	@echo ">> building $*"
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=0 go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/$* ./cmd/$*

.PHONY: test
test: ## 跑全部单测
	@go test -race -count=1 ./...

.PHONY: cover
cover: ## 输出覆盖率
	@go test -race -count=1 -coverprofile=coverage.txt ./...
	@go tool cover -func=coverage.txt | tail -n 1

.PHONY: lint
lint: ## go vet + buf lint
	@go vet ./...
	@$(DOCKER_RUN) $(BUF_IMAGE) lint || true

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

.PHONY: clean
clean: ## 清理构建产物
	@rm -rf $(BIN_DIR) coverage.txt
