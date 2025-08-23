# ---------------------------------------------------------------
# Mizu Example Makefile
#
# Usage:
#   make [target]
#
# Targets:
#   help      Show this help message
#   gen       Generate protocol buffer files
#   clean     Clean up generated files
# ---------------------------------------------------------------

# --- Configuration ---
PREFIX=protogen

# --- Emojis for better output ---
GENERATE_ICON := ✨
CLEAN_ICON := 🧹
DONE_ICON := ✅
HELP_ICON := 💬

# --- Targets ---
.PHONY: all gen clean help
.DEFAULT_GOAL := help

all: gen ## Generate all files (default)

gen: clean ## $(GENERATE_ICON) Generate protocol buffer files
	@echo "$(GENERATE_ICON) Generating protocol buffer files..."
	@buf generate
	@mkdir -p ./${PREFIX}
	@mv dist/* ./${PREFIX}/
	@echo "$(DONE_ICON) Protobuf generation complete."
	@rm -r dist

clean: ## $(CLEAN_ICON) Clean up generated files
	@echo "$(CLEAN_ICON) Cleaning up generated files..."
	@rm -rf dist
	@rm -rf ./${PREFIX}

help: ## $(HELP_ICON) Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
