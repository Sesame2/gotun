BINARY_NAME=gotun
VERSION=0.1.0
BUILD_DIR=./build
LDFLAGS=-ldflags "-X main.Version=$(VERSION)"

# Go命令
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOVET=$(GOCMD) vet
GOLINT=golangci-lint

# 系统信息
GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)

.PHONY: all build clean test lint vet tidy run help

all: lint test build

# 构建可执行文件
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/gotun.go
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# 交叉编译
build-all: build-linux build-windows build-darwin

build-linux:
	@echo "Building for Linux..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_linux_amd64 ./cmd/gotun.go

build-windows:
	@echo "Building for Windows..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_windows_amd64.exe ./cmd/gotun.go

build-darwin:
	@echo "Building for macOS..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_darwin_amd64 ./cmd/gotun.go

# 清理构建文件
clean:
	@echo "Cleaning..."
	@$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete"

# 运行测试
test:
	@echo "Running tests..."
	@$(GOTEST) -v ./...

# 运行代码质量检查
lint:
	@echo "Running linter..."
	@if command -v $(GOLINT) > /dev/null; then \
	    $(GOLINT) run; \
	else \
	    echo "golangci-lint not installed. Skipping lint."; \
	fi

# 运行静态代码分析
vet:
	@echo "Running vet..."
	@$(GOVET) ./...

# 更新依赖
tidy:
	@echo "Tidying modules..."
	@$(GOMOD) tidy

# 运行程序
run:
	@$(GOCMD) run ./cmd/gotun.go

# 显示帮助信息
help:
	@echo "Make targets:"
	@echo "  build      - Build gotun binary"
	@echo "  build-all  - Build for multiple platforms"
	@echo "  clean      - Remove build artifacts"
	@echo "  test       - Run tests"
	@echo "  lint       - Run linter"
	@echo "  vet        - Run go vet"
	@echo "  tidy       - Tidy go modules"
	@echo "  run        - Run the application"
	@echo "  help       - Show this help"