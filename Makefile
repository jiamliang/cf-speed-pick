# Makefile for cf-speed-pick
# 用法:
#   make build         # 当前平台构建
#   make build-amd64   # Linux x86_64
#   make build-arm64   # Linux ARM64 (华硕 RT-AC86U 路由器)
#   make build-all     # 一次性出 amd64 + arm64
#   make test          # 跑单元测试
#   make clean         # 清产物
#
# 默认用 ./go-docker.sh 在容器里编译，无需本机装 Go。

GO_DOCKER ?= ./go-docker.sh
BIN       := cf-speed-pick
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)

IMAGE     := golang:1.21-alpine
WORKDIR   := /work
MOUNT     := -v "$(PWD):$(WORKDIR)" -w $(WORKDIR)

.PHONY: all build build-amd64 build-arm64 build-all test tidy clean

all: build

build:
	$(GO_DOCKER) build -ldflags="$(LDFLAGS)" -o $(BIN) .

build-amd64:
	docker run --rm $(MOUNT) -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=amd64 \
		$(IMAGE) \
		go build -ldflags="$(LDFLAGS)" -o $(BIN)-linux-amd64 .

build-arm64:
	docker run --rm $(MOUNT) -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=arm64 \
		$(IMAGE) \
		go build -ldflags="$(LDFLAGS)" -o $(BIN)-linux-arm64 .

build-all: build-amd64 build-arm64

test:
	$(GO_DOCKER) test ./...

tidy:
	$(GO_DOCKER) mod tidy

clean:
	rm -f $(BIN) $(BIN)-linux-amd64 $(BIN)-linux-arm64