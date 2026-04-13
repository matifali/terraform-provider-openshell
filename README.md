# Terraform Provider for NVIDIA OpenShell

This provider manages [NVIDIA OpenShell](https://github.com/NVIDIA/OpenShell) resources — sandboxes, credential providers, and inference routing — via the gateway gRPC API.

> **Note:** NVIDIA OpenShell is alpha software. APIs and behavior may change without notice.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.23 (to build the provider plugin)
- A running OpenShell gateway (`openshell gateway start`)

## Building

```sh
go install
```

## Usage

```hcl
terraform {
  required_providers {
    openshell = {
      source = "nvidia/openshell"
    }
  }
}

provider "openshell" {
  gateway_url = "localhost:8443"

  # mTLS (local gateway — certs auto-provisioned by the CLI)
  ca_cert = "~/.openshell/tls/ca.crt"
  cert    = "~/.openshell/tls/tls.crt"
  key     = "~/.openshell/tls/tls.key"
}

# Create a credential provider for Claude Code
resource "openshell_provider" "anthropic" {
  name = "anthropic"
  type = "claude"

  credentials = {
    ANTHROPIC_API_KEY = var.anthropic_api_key
  }
}

# Launch a sandbox running Claude Code
resource "openshell_sandbox" "dev" {
  name      = "dev-sandbox"
  image     = "ghcr.io/nvidia/openshell/sandbox:latest"
  providers = [openshell_provider.anthropic.name]
}

# Configure inference routing
resource "openshell_inference" "local" {
  provider_name = "openai"
  model_id      = "gpt-4o"
}
```

## Resources

| Resource | Description |
|----------|-------------|
| `openshell_sandbox` | Manages sandboxed execution environments for AI agents. |
| `openshell_provider` | Manages credential providers (API keys, tokens). |
| `openshell_inference` | Configures cluster inference routing via `inference.local`. |

## Data Sources

| Data Source | Description |
|-------------|-------------|
| `openshell_sandbox` | Reads a single sandbox by name. |
| `openshell_provider` | Reads a single credential provider by name (no secrets exposed). |
| `openshell_sandboxes` | Lists all sandboxes. |
| `openshell_providers` | Lists all credential providers. |

## Authentication

The provider supports three authentication modes:

1. **mTLS** (default for local gateways) — set `ca_cert`, `cert`, and `key`.
2. **Bearer token** (edge/remote gateways) — set `token`.
3. **Insecure** (local development only) — set `insecure = true`.

All fields can also be set via environment variables: `OPENSHELL_GATEWAY_URL`, `OPENSHELL_CA_CERT`, `OPENSHELL_CERT`, `OPENSHELL_KEY`, `OPENSHELL_TOKEN`.

## Development

This provider was scaffolded from [terraform-provider-scaffolding-framework](https://github.com/hashicorp/terraform-provider-scaffolding-framework).

```sh
# Generate proto bindings (requires protoc + protoc-gen-go + protoc-gen-go-grpc)
cd proto/openshellv1
protoc --proto_path=. --proto_path=/usr/local/include \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  *.proto

# Build
go build ./...

# Use dev overrides for local testing
cat >> ~/.terraformrc <<EOF
provider_installation {
  dev_overrides {
    "nvidia/openshell" = "$(go env GOPATH)/bin"
  }
  direct {}
}
EOF

go install
terraform plan
```

## License

MPL-2.0
