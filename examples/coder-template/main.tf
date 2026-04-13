terraform {
  required_providers {
    coder = {
      source = "coder/coder"
    }
    openshell = {
      source = "nvidia/openshell"
    }
  }
}

locals {
  username = data.coder_workspace_owner.me.name
}

# ---------------------------------------------------------------------------
# Parameters
# ---------------------------------------------------------------------------

data "coder_parameter" "gateway_url" {
  name         = "OpenShell Gateway"
  display_name = "OpenShell Gateway URL"
  description  = "gRPC endpoint of the OpenShell gateway."
  type         = "string"
  default      = "localhost:8443"
  mutable      = false
}

data "coder_parameter" "image" {
  name         = "Sandbox Image"
  display_name = "Container Image"
  description  = "The container image for the sandbox."
  type         = "string"
  default      = "codercom/enterprise-base:ubuntu"
  mutable      = false
}

data "coder_parameter" "gpu" {
  name         = "GPU"
  display_name = "Enable GPU passthrough"
  type         = "bool"
  default      = "false"
  mutable      = false
}

data "coder_parameter" "ai_provider" {
  name         = "AI Provider"
  display_name = "AI Agent Provider"
  description  = "Which AI agent credential provider to attach."
  type         = "string"
  default      = "none"
  option {
    name  = "None"
    value = "none"
  }
  option {
    name  = "Claude (Anthropic)"
    value = "claude"
  }
  option {
    name  = "Codex (OpenAI)"
    value = "openai"
  }
  option {
    name  = "Copilot (GitHub)"
    value = "github"
  }
}

# Sensitive — only prompted when an AI provider is selected.
data "coder_parameter" "ai_api_key" {
  name         = "AI API Key"
  display_name = "API Key for the selected AI provider"
  type         = "string"
  default      = ""
  mutable      = true
  ephemeral    = true
}

# ---------------------------------------------------------------------------
# Provider configuration
# ---------------------------------------------------------------------------

provider "openshell" {
  gateway_url = data.coder_parameter.gateway_url.value

  # For local gateways, the CLI auto-provisions mTLS certs.
  # For remote gateways, set OPENSHELL_TOKEN in the environment.
  insecure = false
}

data "coder_provisioner" "me" {}
data "coder_workspace" "me" {}
data "coder_workspace_owner" "me" {}

# ---------------------------------------------------------------------------
# Credential provider (created only when an AI agent is selected)
# ---------------------------------------------------------------------------

locals {
  has_ai_provider = data.coder_parameter.ai_provider.value != "none"

  # Map the parameter value to OpenShell provider type and env var name.
  provider_config = {
    claude = { type = "claude", key_name = "ANTHROPIC_API_KEY" }
    openai = { type = "openai", key_name = "OPENAI_API_KEY" }
    github = { type = "github", key_name = "GITHUB_TOKEN" }
    none   = { type = "", key_name = "" }
  }

  selected_provider = local.provider_config[data.coder_parameter.ai_provider.value]
}

resource "openshell_provider" "ai" {
  count = local.has_ai_provider ? 1 : 0

  name = "${local.username}-${data.coder_workspace.me.name}-${data.coder_parameter.ai_provider.value}"
  type = local.selected_provider.type

  credentials = {
    (local.selected_provider.key_name) = data.coder_parameter.ai_api_key.value
  }
}

# ---------------------------------------------------------------------------
# Coder agent (runs inside the OpenShell sandbox)
# ---------------------------------------------------------------------------

resource "coder_agent" "main" {
  arch           = data.coder_provisioner.me.arch
  os             = "linux"
  startup_script = <<-EOT
    set -e
    if [ ! -f ~/.init_done ]; then
      cp -rT /etc/skel ~
      touch ~/.init_done
    fi
  EOT

  env = {
    GIT_AUTHOR_NAME     = coalesce(data.coder_workspace_owner.me.full_name, data.coder_workspace_owner.me.name)
    GIT_AUTHOR_EMAIL    = data.coder_workspace_owner.me.email
    GIT_COMMITTER_NAME  = coalesce(data.coder_workspace_owner.me.full_name, data.coder_workspace_owner.me.name)
    GIT_COMMITTER_EMAIL = data.coder_workspace_owner.me.email
  }

  metadata {
    display_name = "CPU Usage"
    key          = "0_cpu_usage"
    script       = "coder stat cpu"
    interval     = 10
    timeout      = 1
  }

  metadata {
    display_name = "RAM Usage"
    key          = "1_ram_usage"
    script       = "coder stat mem"
    interval     = 10
    timeout      = 1
  }

  metadata {
    display_name = "Home Disk"
    key          = "3_home_disk"
    script       = "coder stat disk --path $${HOME}"
    interval     = 60
    timeout      = 1
  }
}

# See https://registry.coder.com/modules/coder/code-server
module "code-server" {
  count    = data.coder_workspace.me.start_count
  source   = "registry.coder.com/coder/code-server/coder"
  version  = "~> 1.0"
  agent_id = coder_agent.main.id
  order    = 1
}

# See https://registry.coder.com/modules/coder/jetbrains
module "jetbrains" {
  count      = data.coder_workspace.me.start_count
  source     = "registry.coder.com/coder/jetbrains/coder"
  version    = "~> 1.1"
  agent_id   = coder_agent.main.id
  agent_name = "main"
  folder     = "/home/coder"
}

# ---------------------------------------------------------------------------
# OpenShell sandbox (replaces docker_container)
#
# This is the compute unit. Instead of a raw Docker container, the
# workspace runs inside an OpenShell sandbox with:
#   - Kernel-level filesystem isolation (Landlock)
#   - Network policy enforcement (allow-list only)
#   - Credential injection via openshell_provider (no env var leaks)
#   - Optional inference routing to self-hosted models
# ---------------------------------------------------------------------------

resource "openshell_sandbox" "workspace" {
  count = data.coder_workspace.me.start_count

  name  = "coder-${local.username}-${lower(data.coder_workspace.me.name)}"
  image = data.coder_parameter.image.value
  gpu   = data.coder_parameter.gpu.value == "true"

  # Attach AI credential provider if selected.
  providers = local.has_ai_provider ? [openshell_provider.ai[0].name] : []

  # The coder agent init script and token are passed as environment
  # variables. The sandbox image entrypoint picks these up and starts
  # the agent, which connects back to the Coder server.
  environment = {
    CODER_AGENT_TOKEN = coder_agent.main.token
    CODER_AGENT_URL   = data.coder_workspace.me.access_url
    INIT_SCRIPT       = coder_agent.main.init_script
  }
}
