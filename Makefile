GO_MK_URL   := https://raw.githubusercontent.com/agoodkind/go-makefile/main/go.mk
GO_MK       := .make/go.mk
GO_MK_CACHE := $(HOME)/.cache/go-makefile/go.mk

BINARY            := stats-gh
CMD               := ./cmd/stats-gh
GO_BUILD_TARGETS  := $(CMD)
GO_INSTALL_TARGET := $(CMD)

STATICCHECK_EXTRA_FLAGS = $(STATICCHECK_EXTRA_CORE_FLAGS) $(STATICCHECK_EXTRA_STRICT_FLAGS)

GO_MK_BOOTSTRAP := $(shell \
	mkdir -p "$(dir $(GO_MK))" "$(dir $(GO_MK_CACHE))"; \
	tmp="$(GO_MK).tmp"; \
	if curl -fsSL --connect-timeout 5 --max-time 10 "$(GO_MK_URL)" -o "$$tmp"; then \
		mv "$$tmp" "$(GO_MK)"; \
		cp "$(GO_MK)" "$(GO_MK_CACHE)"; \
	elif [ -f "$(GO_MK_CACHE)" ]; then \
		rm -f "$$tmp"; \
		cp "$(GO_MK_CACHE)" "$(GO_MK)"; \
	elif [ ! -f "$(GO_MK)" ]; then \
		rm -f "$$tmp"; \
		printf '%s\n' "error: go.mk fetch failed and no cache available" >&2; \
	fi)

$(GO_MK):
	@mkdir -p $(dir $@)
	@if curl -fsSL --connect-timeout 5 --max-time 10 "$(GO_MK_URL)" -o "$@"; then \
		mkdir -p "$(dir $(GO_MK_CACHE))" && cp "$@" "$(GO_MK_CACHE)"; \
	elif [ -f "$(GO_MK_CACHE)" ]; then \
		echo "warning: go.mk fetch failed, using cached version" >&2; \
		cp "$(GO_MK_CACHE)" "$@"; \
	else \
		echo "error: go.mk fetch failed and no cache available" >&2; \
		exit 1; \
	fi

-include $(GO_MK)

.PHONY: generate

check: lint test

generate:
	go run $(CMD) generate

.DEFAULT_GOAL := check
