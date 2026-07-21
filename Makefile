.PHONY: help install-hooks test race-test tidy release

# 🎨 Colors and symbols
BLUE := \033[34m
GREEN := \033[32m
YELLOW := \033[33m
RED := \033[31m
PURPLE := \033[35m
CYAN := \033[36m
RESET := \033[0m

RELEASE_MODULES := mizuconnect mizucue mizudi mizumw mizuoai mizuotel
RELEASE_TAGS = $(VERSION) $(addsuffix /$(VERSION),$(RELEASE_MODULES))


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

test: test-mizumw test-mizuoai test-mizucue test-mizudi test-mizuconnect test-mizuotel ## 🧪 Run mizu tests
	@echo "$(BLUE)🧪 Running mizu tests...$(RESET)"
	@go test ./...
	@echo "$(GREEN)✅ Tests completed!$(RESET)"

test-%:
	@echo "$(BLUE)🧪 Running $* tests...$(RESET)"
	@cd $* && go test ./...
	@echo "$(GREEN)✅ Tests completed!$(RESET)"

race-test: race-test-mizumw race-test-mizuoai race-test-mizucue race-test-mizudi race-test-mizuconnect race-test-mizuotel ## 🏃 Run mizu tests with race detection
	@echo "$(BLUE)🏃 Running mizu tests with race detection...$(RESET)"
	@go test -race ./...
	@echo "$(GREEN)🏁 Race tests completed!$(RESET)"

race-test-%:
	@echo "$(BLUE)🏃 Running $* tests with race detection...$(RESET)"
	@cd $* && go test -race ./...
	@echo "$(GREEN)🏁 Race tests completed!$(RESET)"

tidy: tidy-mizumw tidy-mizuoai tidy-mizucue tidy-mizudi tidy-mizuconnect tidy-mizuotel ## 🧹 Run go mod tidy all over the project
	@echo "$(BLUE)🧹 Running mizu go mod tidy...$(RESET)"
	@go mod tidy
	@echo "$(GREEN)✅ Go mod tidy completed!$(RESET)"

tidy-%:
	@echo "$(BLUE)🧹 Running $* go mod tidy...$(RESET)"
	@cd $* && go mod tidy
	@echo "$(GREEN)✅ Go mod tidy completed!$(RESET)"

release: ## 🚀 Verify, tag, and push a release (VERSION=vX.Y.Z)
	@if ! echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		echo "$(RED)❌ VERSION must use vX.Y.Z format$(RESET)"; \
		exit 1; \
	fi
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "$(RED)❌ Commit or remove worktree changes before releasing$(RESET)"; \
		exit 1; \
	fi
	@$(MAKE) tidy
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "$(RED)❌ go mod tidy changed the worktree; commit those changes first$(RESET)"; \
		exit 1; \
	fi
	@$(MAKE) test
	@$(MAKE) race-test
	@set -eu; \
		branch="$$(git branch --show-current)"; \
		if [ -z "$$branch" ]; then \
			echo "$(RED)❌ Cannot release from a detached HEAD$(RESET)"; \
			exit 1; \
		fi; \
		for tag in $(RELEASE_TAGS); do \
			if git show-ref --verify --quiet "refs/tags/$$tag"; then \
				echo "$(RED)❌ Local tag already exists: $$tag$(RESET)"; \
				exit 1; \
			fi; \
		done; \
		remote_tags="$$(git ls-remote --tags origin $(addprefix refs/tags/,$(RELEASE_TAGS)))"; \
		if [ -n "$$remote_tags" ]; then \
			echo "$(RED)❌ One or more release tags already exist on origin$(RESET)"; \
			echo "$$remote_tags"; \
			exit 1; \
		fi; \
		created=""; \
		cleanup() { \
			for tag in $$created; do git tag -d "$$tag" >/dev/null 2>&1 || true; done; \
		}; \
		trap cleanup 0 1 2 15; \
		for tag in $(RELEASE_TAGS); do \
			git tag "$$tag"; \
			created="$$created $$tag"; \
		done; \
		git push --atomic origin "HEAD:refs/heads/$$branch" $(addprefix refs/tags/,$(RELEASE_TAGS)); \
		trap - 0 1 2 15
	@echo "$(GREEN)🚀 Released $(VERSION)$(RESET)"
