GO   := go
MYGO := $(GO) tool mygo

# Target of make build: a GOOS/GOARCH pair, or comma separated pairs, or
# web. Defaults to the machine's own.
PLATFORM ?= $(shell $(GO) env GOOS)/$(shell $(GO) env GOARCH)

# Go files outside the generated build output.
FILES := $(shell find . -name '*.go' -not -path './.mygo/*' -not -path './build/*')

.PHONY: dev run web build test vet fmt check clean

dev: ## 开发模式：构建并启动原生 app，改代码自动重启
	$(MYGO) dev

run: ## 直接运行一次（Ctrl+C 结束）
	$(GO) run .

web: ## 在浏览器里开发（wasm，http://localhost:5174）
	$(MYGO) dev -web

build: ## 打包到 build/（make build PLATFORM=linux/amd64,windows/amd64）
	$(MYGO) build -platform "$(PLATFORM)" $(ARGS)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...
	$(MYGO) vet .

fmt:
	$(GO) fmt ./...

check: ## 格式、vet、测试全跑一遍
	@test -z "$$(gofmt -l $(FILES))" || { gofmt -l $(FILES); echo "以上文件未 gofmt"; exit 1; }
	$(GO) vet ./...
	$(MYGO) vet .
	$(GO) test ./...

clean:
	rm -rf build .mygo
