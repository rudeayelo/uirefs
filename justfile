set shell := ["bash", "-cu"]

port := env_var_or_default("PORT", "4880")

# Vet and run all tests
check:
    go vet ./...
    go test ./...

# Static single binary (no cgo)
build:
    CGO_ENABLED=0 go build -ldflags="-s -w" -o uirefs ./cmd/uirefs

# Run on 127.0.0.1 with a throwaway data dir
dev:
    CGO_ENABLED=0 go run ./cmd/uirefs serve --port {{port}} --data ./data

# Create a worktree under worktrees/<name> on a new branch <name>
worktree name:
    git worktree add worktrees/{{name}} -b {{name}}

# Install the binary, the user service and the MCP server (see install.sh)
install:
    ./install.sh

# Stop and remove the service, the MCP entries and the binary
uninstall:
    ./uninstall.sh
