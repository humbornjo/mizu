.PHONY: help install-hooks test race-test

# 🎨 Colors and symbols
BLUE := \033[34m
GREEN := \033[32m
YELLOW := \033[33m
RED := \033[31m
PURPLE := \033[35m
CYAN := \033[36m
RESET := \033[0m


help: ## 📚 Show this help message
	@echo "$(BLUE)🌊 Mizu HTTP Framework - Development Tools$(RESET)"
	@echo "$(PURPLE)═══════════════════════════════════════════$(RESET)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(CYAN)%-15s$(RESET) %s\n", $$1, $$2}'
	@echo ""

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

test: test-mizuoai test-mizudi test-mizuconnect test-mizuotel ## 🧪 Run mizu tests
	@echo "$(BLUE)🧪 Running mizu tests...$(RESET)"
	@go test ./...
	@echo "$(GREEN)✅ Tests completed!$(RESET)"

test-%:
	@echo "$(BLUE)🧪 Running $* tests...$(RESET)"
	@cd $* && go test ./...
	@echo "$(GREEN)✅ Tests completed!$(RESET)"

race-test: race-test-mizuoai race-test-mizudi race-test-mizuconnect race-test-mizuotel ## 🏃 Run mizu tests with race detection
	@echo "$(BLUE)🏃 Running mizu tests with race detection...$(RESET)"
	@go test -race ./...
	@echo "$(GREEN)🏁 Race tests completed!$(RESET)"

race-test-%:
	@echo "$(BLUE)🏃 Running $* tests with race detection...$(RESET)"
	@cd $* && go test -race ./...
	@echo "$(GREEN)🏁 Race tests completed!$(RESET)"

tidy: tidy-mizuoai tidy-mizudi tidy-mizuconnect tidy-mizuotel ## 🧹 Run mizu tests with race detection
	@echo "$(BLUE)🧹 Running mizu go mod tidy...$(RESET)"
	@go mod tidy
	@echo "$(GREEN)✅ Go mod tidy completed!$(RESET)"

tidy-%:
	@echo "$(BLUE)🧹 Running $* go mod tidy...$(RESET)"
	@cd $* && go mod tidy
	@echo "$(GREEN)✅ Go mod tidy completed!$(RESET)"
