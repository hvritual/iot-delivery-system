# Canonical Yunka project workflow. The framework source and protobuf include
# are fixed by the repository gitlink; callers may override tool locations,
# but never the reviewed framework identity. PROJECT_ROOT is absolute because
# the CLI invocation runs through the Yunka app module.
override PROJECT_ROOT := $(abspath $(CURDIR))
override REPOSITORY_ROOT := $(abspath $(PROJECT_ROOT)/..)
override YUNKA_REVISION := 057ebcf88a87303eb633eb6e604d306f633dfac0
GO ?= go
TOOLS_DIR ?= .tools
PROTOC ?= $(TOOLS_DIR)/protoc-21.12/bin/protoc
PROTOC_GEN_GO ?= $(TOOLS_DIR)/bin/protoc-gen-go
PROTOC_GEN_GO_GRPC ?= $(TOOLS_DIR)/bin/protoc-gen-go-grpc
override YUNKA_CLI = "$(GO)" -C "$(REPOSITORY_ROOT)/third_party/yunka/app" run ./cmd
override YUNKA_PROTO_PATH := $(REPOSITORY_ROOT)/third_party/yunka/contracts/proto

.PHONY: yunka-revision-check yunka-cli-check yunka-toolchain-check yunka-context yunka-generate yunka-check yunka-verify

yunka-revision-check:
	@test "$$(git -C "$(REPOSITORY_ROOT)" ls-tree HEAD -- third_party/yunka | awk '{print $$3}')" = "$(YUNKA_REVISION)" || { echo "Repository HEAD must pin third_party/yunka to $(YUNKA_REVISION)"; exit 1; }
	@test "$$(git -C "$(REPOSITORY_ROOT)/third_party/yunka" rev-parse HEAD)" = "$(YUNKA_REVISION)" || { echo "Materialized third_party/yunka must be $(YUNKA_REVISION)"; exit 1; }
	@test -z "$$(git -C "$(REPOSITORY_ROOT)/third_party/yunka" status --porcelain)" || { echo "Materialized third_party/yunka must be clean"; exit 1; }
	@test -f "$(YUNKA_PROTO_PATH)/yunka/dsl/v1/options.proto" || { echo "Pinned Yunka protobuf include is incomplete"; exit 1; }

yunka-cli-check: yunka-revision-check
	@test "$$("$(GO)" version | awk '{print $$3}')" = "go1.25.13" || { echo "Yunka workflow requires Go 1.25.13"; exit 1; }

yunka-toolchain-check: yunka-cli-check
	@test "$$("$(PROTOC)" --version)" = "libprotoc 3.21.12" || { echo "Yunka workflow requires protoc 3.21.12"; exit 1; }
	@test "$$("$(PROTOC_GEN_GO)" --version)" = "protoc-gen-go v1.36.11" || { echo "Yunka workflow requires protoc-gen-go v1.36.11"; exit 1; }
	@test "$$("$(PROTOC_GEN_GO_GRPC)" --version)" = "protoc-gen-go-grpc 1.6.2" || { echo "Yunka workflow requires protoc-gen-go-grpc 1.6.2"; exit 1; }

yunka-context: yunka-cli-check
	$(YUNKA_CLI) context --root "$(PROJECT_ROOT)" --json

yunka-generate: yunka-toolchain-check
	PROTOC_GEN_GO="$(PROTOC_GEN_GO)" PROTOC_GEN_GO_GRPC="$(PROTOC_GEN_GO_GRPC)" $(YUNKA_CLI) generate --root "$(PROJECT_ROOT)" --full --protoc "$(PROTOC)" --proto-path "$(YUNKA_PROTO_PATH)"

yunka-check: yunka-toolchain-check
	PROTOC_GEN_GO="$(PROTOC_GEN_GO)" PROTOC_GEN_GO_GRPC="$(PROTOC_GEN_GO_GRPC)" $(YUNKA_CLI) check --root "$(PROJECT_ROOT)" --full --protoc "$(PROTOC)" --proto-path "$(YUNKA_PROTO_PATH)"

yunka-verify: yunka-check
