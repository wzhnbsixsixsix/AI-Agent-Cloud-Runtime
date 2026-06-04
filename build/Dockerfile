# syntax=docker/dockerfile:1.7
# AgentForge 多阶段构建：
#   Stage 1: 用 buf 镜像把 .proto 生成到 pkg/proto/gen
#   Stage 2: 用 golang 镜像 go mod tidy + go build
#   Stage 3: distroless 运行时

# ---------------- proto-gen ----------------
FROM bufbuild/buf:1.45.0 AS bufgen
WORKDIR /src
COPY buf.yaml buf.gen.yaml ./
COPY pkg/proto ./pkg/proto
RUN buf generate

# ---------------- builder ----------------
FROM golang:1.22-alpine AS builder
ARG BIN=gateway
ENV CGO_ENABLED=0 GOFLAGS=-mod=mod
WORKDIR /src

# 先复制 module 文件并预热依赖（构建缓存友好）
COPY go.mod ./
COPY go.sum* ./
RUN go mod download || true

# 拷贝源码 + 生成的 proto
COPY . .
COPY --from=bufgen /src/pkg/proto/gen ./pkg/proto/gen

RUN go mod tidy
RUN go build -trimpath -ldflags='-s -w' -o /out/app ./cmd/${BIN}

# ---------------- runtime ----------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
