BINARY_NAME=gotun
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DIR=./build
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -s -w"
MAIN_PACKAGE=./cmd/gotun

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

# 发布平台和架构
PLATFORMS=linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64

.PHONY: all build clean test lint vet tidy run help build-all build-release

all: lint test build

# 构建可执行文件
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# 交叉编译
build-all: build-linux build-windows build-darwin

build-linux:
	@echo "Building for Linux..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_linux_amd64 $(MAIN_PACKAGE)

build-windows:
	@echo "Building for Windows..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_windows_amd64.exe $(MAIN_PACKAGE)

build-darwin:
	@echo "Building for macOS..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)_darwin_amd64 $(MAIN_PACKAGE)

# 发布构建 - 支持多架构
build-release:
	@echo "Building release for version: $(VERSION)"
	@mkdir -p $(BUILD_DIR)
	@for platform in $(PLATFORMS); do \
	    os=$$(echo $$platform | cut -d'/' -f1); \
	    arch=$$(echo $$platform | cut -d'/' -f2); \
	    echo "Building $$os/$$arch..."; \
	    if [ "$$os" = "windows" ]; then \
	        GOOS=$$os GOARCH=$$arch $(GOBUILD) $(LDFLAGS) \
	            -o $(BUILD_DIR)/$(BINARY_NAME)_$(VERSION)_$${os}_$${arch}.exe $(MAIN_PACKAGE); \
	    else \
	        GOOS=$$os GOARCH=$$arch $(GOBUILD) $(LDFLAGS) \
	            -o $(BUILD_DIR)/$(BINARY_NAME)_$(VERSION)_$${os}_$${arch} $(MAIN_PACKAGE); \
	    fi; \
	done
	@echo "Release build complete for version: $(VERSION)"
	@echo "Built files:"
	@ls -la $(BUILD_DIR)/$(BINARY_NAME)_$(VERSION)_*

# 打包发布文件
package-release: build-release
	@echo "Packaging release files..."
	@cd $(BUILD_DIR) && \
	for file in $(BINARY_NAME)_$(VERSION)_*; do \
	    if [ -f "$$file" ]; then \
	        os_arch=$$(echo $$file | sed 's/$(BINARY_NAME)_$(VERSION)_//'); \
	        if [[ "$$file" == *.exe ]]; then \
	            zip "$$file.zip" "$$file"; \
	        else \
	            tar -czf "$$file.tar.gz" "$$file"; \
	        fi; \
	        echo "Packaged: $$file"; \
	    fi; \
	done
	@echo "Packaging complete"

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
	@$(GOCMD) run $(MAIN_PACKAGE)

# 显示版本信息
version:
	@echo "Current version: $(VERSION)"

# 显示帮助信息
help:
	@echo "Make targets:"
	@echo "  build         - Build gotun binary for current platform"
	@echo "  build-all     - Build for multiple platforms (amd64 only)"
	@echo "  build-release - Build release versions for all platforms and architectures"
	@echo "  package-release - Build and package release files"
	@echo "  clean         - Remove build artifacts"
	@echo "  test          - Run tests"
	@echo "  lint          - Run linter"
	@echo "  vet           - Run go vet"
	@echo "  tidy          - Tidy go modules"
	@echo "  run           - Run the application"
	@echo "  version       - Show current version"
	@echo "  help          - Show this help"
	@echo ""
	@echo "Release workflow:"
	@echo "  1. Tag your commit: git tag v1.0.0"
	@echo "  2. Build release:   make build-release"
	@echo "  3. Package files:   make package-release"