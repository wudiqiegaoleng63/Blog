SHELL := /bin/sh

ROOT_DIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
BACKEND_DIR := $(ROOT_DIR)/backend
BIN_DIR ?= $(ROOT_DIR)/bin
ENV_FILE ?= $(ROOT_DIR)/.env
COMPOSE_FILE := $(ROOT_DIR)/deploy/compose.yaml
COMPOSE_DEV_FILE := $(ROOT_DIR)/deploy/compose.dev.yaml
GO ?= go
DOCKER_COMPOSE ?= docker compose
COMPOSE = $(DOCKER_COMPOSE) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)"

.PHONY: help fmt fmt-check test vet build check migrate-list compose-config compose-build up dev-up down logs ps

help: ## 显示可用命令
	@printf '%s\n' \
	  'Stage 0 project commands (run from the project root)' \
	  '' \
	  '  make help             Show this help' \
	  '  make fmt              Format all Go source files' \
	  '  make test             Run all Go tests' \
	  '  make vet              Run go vet' \
	  '  make build            Build api, worker, and migrate into ./bin' \
	  '  make check            Check formatting, vet, tests, and builds' \
	  '  make migrate-list     List embedded SQL migration versions' \
	  '  make compose-config   Validate Compose without printing secrets' \
	  '  make compose-build    Build Compose application images' \
	  '  make up               Build and start the base Compose stack' \
	  '  make dev-up           Start with loopback debug ports enabled' \
	  '  make down             Stop containers; preserve named volumes' \
	  '  make logs             Follow logs from all services' \
	  '  make ps               Show Compose service status' \
	  '' \
	  'Variables:' \
	  '  ENV_FILE=/path/to/file       default: ./.env' \
	  '  BIN_DIR=/path/to/output      default: ./bin' \
	  '  GO=/path/to/go               default: go' \
	  '  DOCKER_COMPOSE="..."         default: docker compose'

fmt: ## 格式化 Go 源码
	@cd "$(BACKEND_DIR)" && files="$$(find . -type f -name '*.go' -not -path './vendor/*')"; \
	if [ -n "$$files" ]; then gofmt -w $$files; fi

fmt-check: ## 检查 Go 格式但不修改文件
	@cd "$(BACKEND_DIR)" && files="$$(find . -type f -name '*.go' -not -path './vendor/*')"; \
	unformatted="$$(gofmt -l $$files)"; \
	if [ -n "$$unformatted" ]; then printf 'Unformatted Go files:\n%s\n' "$$unformatted" >&2; exit 1; fi

test: ## 运行 Go 测试
	@cd "$(BACKEND_DIR)" && "$(GO)" test ./...

vet: ## 运行 go vet
	@cd "$(BACKEND_DIR)" && "$(GO)" vet ./...

build: ## 构建三个后端命令
	@mkdir -p "$(BIN_DIR)"
	@cd "$(BACKEND_DIR)" && for command in api worker migrate; do \
		"$(GO)" build -trimpath -o "$(BIN_DIR)/$$command" "./cmd/$$command" || exit $$?; \
	done

check: fmt-check vet test build ## 执行阶段 0 本地质量检查

migrate-list: ## 列出内嵌迁移；不连接数据库
	@cd "$(BACKEND_DIR)" && "$(GO)" run ./cmd/migrate list

compose-config: ## 静默校验 Compose，避免把 secret 打到终端
	@test -f "$(ENV_FILE)" || { printf 'Missing ENV_FILE: %s\nCopy .env.example to .env first.\n' "$(ENV_FILE)" >&2; exit 1; }
	@$(COMPOSE) config --quiet
	@printf 'Compose configuration is valid: %s\n' "$(COMPOSE_FILE)"

compose-build: ## 构建 Compose 后端镜像
	@test -f "$(ENV_FILE)" || { printf 'Missing ENV_FILE: %s\n' "$(ENV_FILE)" >&2; exit 1; }
	@$(COMPOSE) build

up: ## 启动基础栈；一次性 migrate 会先执行
	@test -f "$(ENV_FILE)" || { printf 'Missing ENV_FILE: %s\nCopy .env.example to .env and replace secrets first.\n' "$(ENV_FILE)" >&2; exit 1; }
	@$(COMPOSE) up -d --build

dev-up: ## 启动栈并将 API/MySQL/Redis 调试端口绑定到 loopback
	@test -f "$(ENV_FILE)" || { printf 'Missing ENV_FILE: %s\nCopy .env.example to .env and replace secrets first.\n' "$(ENV_FILE)" >&2; exit 1; }
	@$(COMPOSE) -f "$(COMPOSE_DEV_FILE)" up -d --build

down: ## 停止栈但保留 mysql_data 和 redis_data
	@test -f "$(ENV_FILE)" || { printf 'Missing ENV_FILE: %s\n' "$(ENV_FILE)" >&2; exit 1; }
	@$(COMPOSE) down

logs: ## 持续查看服务日志
	@test -f "$(ENV_FILE)" || { printf 'Missing ENV_FILE: %s\n' "$(ENV_FILE)" >&2; exit 1; }
	@$(COMPOSE) logs -f

ps: ## 查看服务状态
	@test -f "$(ENV_FILE)" || { printf 'Missing ENV_FILE: %s\n' "$(ENV_FILE)" >&2; exit 1; }
	@$(COMPOSE) ps
