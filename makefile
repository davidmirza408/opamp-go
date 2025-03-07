# Function to execute a command.
# Accepts command to execute as first parameter.
define exec-command
$(1)

endef

TOOLS_MOD_DIR := ./internal/tools

# Find all .proto files.
BASELINE_PROTO_FILES := $(wildcard internal/proto/*.proto)

all: test build-examples

.PHONY: test
test:
	go test -race ./...
	cd internal/examples && go test -race ./...

.PHONY: test-with-cover
test-with-cover:
	go-acc --output=coverage.out --ignore=protobufs ./...

show-coverage: test-with-cover
	# Show coverage as HTML in the default browser.
	go tool cover -html=coverage.out

.PHONY: build-examples
build-examples: build-example-agent build-example-supervisor build-example-server

build-example-agent:
	cd internal/examples && go build -o agent/bin/agent agent/main.go

build-example-supervisor:
	cd internal/examples && go build -o supervisor/bin/supervisor supervisor/main.go

build-example-server:
	cd internal/examples && go build -o server/bin/server server/main.go

run-examples: build-examples
	cd internal/examples/server && ./bin/server &
	@echo Server UI is running at http://localhost:4321/
	cd internal/examples/agent && ./bin/agent

run-server: build-example-server
	cd internal/examples/server && ./bin/server &
	@echo Server UI is running at http://localhost:4321/

# Generate Protobuf Go files.
.PHONY: gen-proto
gen-proto:
	$(foreach file,$(BASELINE_PROTO_FILES),$(call exec-command,docker run --rm -v${PWD}:${PWD} \
        -w${PWD} otel/build-protobuf:latest --proto_path=${PWD}/internal/proto/ \
        --go_out=${PWD}/internal/proto/ -I${PWD}/internal/proto/ ${PWD}/$(file)))

	cp -R internal/proto/github.com/open-telemetry/opamp-go/protobufs/* protobufs/
	rm -rf internal/proto/github.com/

.PHONY: gomoddownload
gomoddownload:
	go mod download

.PHONY: install-tools
install-tools:
	cd $(TOOLS_MOD_DIR) && go install github.com/ory/go-acc
