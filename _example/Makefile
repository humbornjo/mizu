# --- Configuration ---
PREFIX=protogen

# --- Emojis for better output ---
GENERATE_ICON := ✨
CLEAN_ICON := 🧹
DONE_ICON := ✅

# --- Targets ---
.PHONY: all gen clean help
.DEFAULT_GOAL := help

all: gen ## Generate all files (default)

gen: clean ## Generate protocol buffer files
	@echo "$(GENERATE_ICON) Generating protocol buffer files..."
	@buf generate
	@echo 'package '$(PREFIX)'\n\n'    \
		'import (\n'                  \
		'  _ "embed"\n'               \
		')\n\n'                       \
		'var (\n'                     \
		'  //go:embed openapi.yaml\n' \
		'  OPENAPI []byte\n'          \
		')' >  dist/const.go
	@mkdir -p ./${PREFIX}
	@mv dist/* ./${PREFIX}/
	@echo "$(DONE_ICON) Protobuf generation complete."
	@rm -r dist

clean: ## Clean up generated files
	@echo "$(CLEAN_ICON) Cleaning up generated files..."
	@rm -rf dist
	@rm -rf ./${PREFIX}

run: ## Run example
	@go run .

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
