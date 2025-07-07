# 🌊 Mizu HTTP Framework - Development Tools
# ============================================

.PHONY: help lint fmt test clean check-deps install-deps all

# 🎨 Colors and symbols
BLUE := \033[34m
GREEN := \033[32m
YELLOW := \033[33m
RED := \033[31m
PURPLE := \033[35m
CYAN := \033[36m
RESET := \033[0m

# 📦 Project info
PROJECT_NAME := mizu
GO_VERSION := 1.24

# 🎯 Default target
help: ## 📚 Show this help message
	@echo "$(BLUE)🌊 Mizu HTTP Framework - Development Tools$(RESET)"
	@echo "$(PURPLE)═══════════════════════════════════════════$(RESET)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(CYAN)%-15s$(RESET) %s\n", $$1, $$2}'
	@echo ""

all: deps fmt lint test ## 🚀 Run all checks (format, lint, test)
	@echo "$(GREEN)✅ All checks completed successfully!$(RESET)"

# 🔧 Dependencies
check-deps: ## 🔍 Check if required tools are installed
	@echo "$(BLUE)🔍 Checking dependencies...$(RESET)"
	@command -v go >/dev/null 2>&1 || { echo "$(RED)❌ Go is not installed$(RESET)"; exit 1; }
	@command -v golangci-lint >/dev/null 2>&1 || { echo "$(YELLOW)⚠️  golangci-lint not found - run 'make install-deps'$(RESET)"; }
	@echo "$(GREEN)✅ Dependencies check completed$(RESET)"

install-deps: ## 📥 Install development dependencies
	@echo "$(BLUE)📥 Installing development dependencies...$(RESET)"
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "$(YELLOW)⬇️  Installing golangci-lint...$(RESET)"; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	else \
		echo "$(GREEN)✅ golangci-lint already installed$(RESET)"; \
	fi
	@echo "$(GREEN)🎉 Dependencies installed successfully!$(RESET)"

deps: check-deps ## 🔗 Download Go modules
	@echo "$(BLUE)📦 Downloading Go modules...$(RESET)"
	@go mod download
	@go mod tidy
	@echo "$(GREEN)✅ Modules updated$(RESET)"

# 🪝 Git Hooks
install-hooks: ## 🪝 Install Git hooks
	@echo "$(BLUE)🪝 Installing Git hooks...$(RESET)"
	@if [ ! -d ".git" ]; then \
		echo "$(RED)❌ Not in a git repository$(RESET)"; \
		exit 1; \
	fi
	@if [ -f ".pre-commit" ]; then \
		cp .pre-commit .git/hooks/pre-commit; \
		chmod +x .git/hooks/pre-commit; \
		echo "$(GREEN)✅ Pre-commit hook installed$(RESET)"; \
	else \
		echo "$(RED)❌ .pre-commit not found$(RESET)"; \
		exit 1; \
	fi
	@if [ -f ".commit-msg" ]; then \
		cp .commit-msg .git/hooks/commit-msg; \
		chmod +x .git/hooks/commit-msg; \
		echo "$(GREEN)✅ Commit-msg hook installed$(RESET)"; \
	else \
		echo "$(RED)❌ .commit-msg not found$(RESET)"; \
		exit 1; \
	fi
	@echo "$(GREEN)🎉 Git hooks installed successfully!$(RESET)"
	@echo "$(YELLOW)💡 The commit-msg hook will validate conventional commit message format$(RESET)"
	@echo "$(YELLOW)💡 The pre-commit hook will now run 'make format' and 'make lint' before each commit$(RESET)"

# 🎨 Formatting
format: fmt ## 🎨 Alias for fmt (used by pre-commit hook)

fmt: ## 🎨 Format Go code
	@echo "$(BLUE)🎨 Formatting Go code...$(RESET)"
	@echo "$(YELLOW)  📝 Running go fmt...$(RESET)"
	@go fmt ./...
	@echo "$(YELLOW)  🔧 Running go mod tidy...$(RESET)"
	@go mod tidy
	@if command -v goimports >/dev/null 2>&1; then \
		echo "$(YELLOW)  📋 Running goimports...$(RESET)"; \
		goimports -w .; \
	fi
	@echo "$(GREEN)✨ Code formatting completed!$(RESET)"

fmt-check: ## 🔍 Check if code is properly formatted
	@echo "$(BLUE)🔍 Checking code formatting...$(RESET)"
	@if [ -n "$$(go fmt ./...)" ]; then \
		echo "$(RED)❌ Code is not properly formatted$(RESET)"; \
		echo "$(YELLOW)💡 Run 'make fmt' to fix formatting$(RESET)"; \
		exit 1; \
	fi
	@echo "$(GREEN)✅ Code is properly formatted$(RESET)"

# 🔍 Linting
lint: check-deps ## 🔍 Run golangci-lint
	@echo "$(BLUE)🔍 Running golangci-lint...$(RESET)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "$(YELLOW)  📍 Config path: $$(golangci-lint config path)$(RESET)"; \
		echo "$(YELLOW)  🔧 Verifying config...$(RESET)"; \
		golangci-lint config verify; \
		if [ $$? -eq 0 ]; then \
			echo "$(GREEN)  ✅ Config verified$(RESET)"; \
			golangci-lint run --timeout=5m; \
			if [ $$? -eq 0 ]; then \
				echo "$(GREEN)✅ Linting passed!$(RESET)"; \
			else \
				echo "$(RED)❌ Linting failed$(RESET)"; \
				exit 1; \
			fi \
		else \
			echo "$(RED)❌ Config verification failed$(RESET)"; \
			exit 1; \
		fi \
	else \
		echo "$(RED)❌ golangci-lint not found$(RESET)"; \
		echo "$(YELLOW)💡 Run 'make install-deps' first$(RESET)"; \
		exit 1; \
	fi

lint-fix: check-deps ## 🔧 Run golangci-lint with auto-fix
	@echo "$(BLUE)🔧 Running golangci-lint with auto-fix...$(RESET)"
	@golangci-lint run --fix --timeout=5m
	@echo "$(GREEN)🛠️  Auto-fix completed!$(RESET)"

# 🧪 Testing
test: ## 🧪 Run tests
	@echo "$(BLUE)🧪 Running tests...$(RESET)"
	@go test -v ./...
	@echo "$(GREEN)✅ Tests completed!$(RESET)"

test-coverage: ## 📊 Run tests with coverage
	@echo "$(BLUE)📊 Running tests with coverage...$(RESET)"
	@go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)📈 Coverage report generated: coverage.html$(RESET)"

test-race: ## 🏃 Run tests with race detection
	@echo "$(BLUE)🏃 Running tests with race detection...$(RESET)"
	@go test -race ./...
	@echo "$(GREEN)🏁 Race tests completed!$(RESET)"

# 🏗️ Build
build: ## 🏗️ Build the project
	@echo "$(BLUE)🏗️ Building project...$(RESET)"
	@go build ./...
	@echo "$(GREEN)🎯 Build completed!$(RESET)"

# 🧹 Cleanup
clean: ## 🧹 Clean build artifacts and cache
	@echo "$(BLUE)🧹 Cleaning up...$(RESET)"
	@go clean -cache -testcache -modcache
	@rm -f coverage.out coverage.html
	@echo "$(GREEN)✨ Cleanup completed!$(RESET)"

# 🚨 CI/Security
sec: ## 🔒 Run security checks
	@echo "$(BLUE)🔒 Running security checks...$(RESET)"
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "$(YELLOW)⚠️  govulncheck not found - installing...$(RESET)"; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
		govulncheck ./...; \
	fi
	@echo "$(GREEN)🛡️  Security check completed!$(RESET)"

ci: deps fmt-check lint test ## 🤖 Run CI pipeline (format check, lint, test)
	@echo "$(GREEN)🤖 CI pipeline completed successfully!$(RESET)"

# 📈 Advanced
bench: ## 📈 Run benchmarks
	@echo "$(BLUE)📈 Running benchmarks...$(RESET)"
	@go test -bench=. -benchmem ./...
	@echo "$(GREEN)⚡ Benchmarks completed!$(RESET)"

mod-update: ## 🔄 Update Go modules to latest versions
	@echo "$(BLUE)🔄 Updating Go modules...$(RESET)"
	@go get -u ./...
	@go mod tidy
	@echo "$(GREEN)📦 Modules updated to latest versions!$(RESET)"

# 📊 Project stats
stats: ## 📊 Show project statistics
	@echo "$(BLUE)📊 Project Statistics$(RESET)"
	@echo "$(PURPLE)═══════════════════$(RESET)"
	@echo "$(CYAN)📁 Lines of code:$(RESET) $$(find . -name '*.go' -not -path './vendor/*' | xargs wc -l | tail -1 | awk '{print $$1}')"
	@echo "$(CYAN)📂 Go files:$(RESET) $$(find . -name '*.go' -not -path './vendor/*' | wc -l)"
	@echo "$(CYAN)📦 Packages:$(RESET) $$(go list ./... | wc -l)"
	@echo "$(CYAN)🧪 Test files:$(RESET) $$(find . -name '*_test.go' | wc -l)"
