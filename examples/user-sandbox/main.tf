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

# ---------------------------------------------------------------------------
# Template: User-facing coding agent sandbox
#
# The developer launches Claude Code, Codex, or any coding agent inside
# an OpenShell sandbox. The sandbox provides:
#   - Filesystem isolation (Landlock) — agent can't read ~/.ssh, etc.
#   - Network allow-list — agent can only reach approved endpoints
#   - Credential proxy — API keys are never visible inside the sandbox
#   - Inference routing — LLM calls can be routed to self-hosted models
#
# The developer connects via SSH / VS Code / JetBrains as usual.
# When they run `claude` or `codex` inside the workspace, it just works
# because OpenShell's credential proxy transparently injects the API key.
# ---------------------------------------------------------------------------

locals {
  username = data.coder_workspace_owner.me.name
}

# ---------------------------------------------------------------------------
# Parameters
# ---------------------------------------------------------------------------

data "coder_parameter" "gateway_url" {
  name         = "openshell_gateway"
  display_name = "OpenShell Gateway"
  description  = "gRPC endpoint of the OpenShell gateway."
  type         = "string"
  default      = "localhost:8443"
  mutable      = false
}

data "coder_parameter" "image" {
  name         = "image"
  display_name = "Container Image"
  type         = "string"
  default      = "codercom/enterprise-base:ubuntu"
  mutable      = false
}

data "coder_parameter" "gpu" {
  name         = "gpu"
  display_name = "Enable GPU"
  description  = "Pass host GPUs into the sandbox for local inference."
  type         = "bool"
  default      = "false"
  mutable      = false
}

data "coder_parameter" "coding_agent" {
  name         = "coding_agent"
  display_name = "Coding Agent"
  description  = "Which coding agent to pre-configure. OpenShell auto-creates the credential provider and injects it into the sandbox."
  type         = "string"
  default      = "none"
  option {
    name  = "None"
    value = "none"
  }
  option {
    name  = "Claude Code"
    value = "claude"
  }
  option {
    name  = "Codex (OpenAI)"
    value = "openai"
  }
  option {
    name  = "GitHub Copilot"
    value = "github"
  }
}

# Only shown when a coding agent is selected.
data "coder_parameter" "api_key" {
  name         = "api_key"
  display_name = "API Key"
  description  = "API key for the selected coding agent. Injected via OpenShell's credential proxy — the agent never sees the real key."
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
  # mTLS certs auto-resolved from ~/.openshell/tls/ for local gateways.
  # For remote gateways, set OPENSHELL_TOKEN in the provisioner env.
}

data "coder_provisioner" "me" {}
data "coder_workspace" "me" {}
data "coder_workspace_owner" "me" {}

# ---------------------------------------------------------------------------
# Credential provider
#
# Created only when the developer selects a coding agent. OpenShell stores
# the API key on the gateway and injects a placeholder token into the
# sandbox at runtime. The coding agent (Claude, Codex, etc.) sees the
# placeholder in its environment and uses it normally. When the agent
# makes an API call, OpenShell's proxy swaps the placeholder for the
# real key before forwarding. The real key never touches the sandbox.
# ---------------------------------------------------------------------------

locals {
  has_agent = data.coder_parameter.coding_agent.value != "none"

  agent_config = {
    claude = { type = "claude", env_var = "ANTHROPIC_API_KEY" }
    openai = { type = "openai", env_var = "OPENAI_API_KEY" }
    github = { type = "github", env_var = "GITHUB_TOKEN" }
    none   = { type = "", env_var = "" }
  }

  selected = local.agent_config[data.coder_parameter.coding_agent.value]
}

resource "openshell_provider" "agent" {
  count = local.has_agent ? 1 : 0

  name = "${local.username}-${data.coder_workspace.me.name}-${local.selected.type}"
  type = local.selected.type

  credentials = {
    (local.selected.env_var) = data.coder_parameter.api_key.value
  }
}

# ---------------------------------------------------------------------------
# Coder agent
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

module "code-server" {
  count    = data.coder_workspace.me.start_count
  source   = "registry.coder.com/coder/code-server/coder"
  version  = "~> 1.0"
  agent_id = coder_agent.main.id
  order    = 1
}

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
# The developer's workspace runs inside an OpenShell sandbox. When they
# run `claude` or `codex`, the tool auto-detects the API key placeholder
# in the environment and works out of the box. OpenShell's proxy handles
# credential resolution transparently — no configuration needed.
# ---------------------------------------------------------------------------

resource "openshell_sandbox" "workspace" {
  count = data.coder_workspace.me.start_count

  name  = "coder-${local.username}-${lower(data.coder_workspace.me.name)}"
  image = data.coder_parameter.image.value
  gpu   = data.coder_parameter.gpu.value == "true"

  providers = local.has_agent ? [openshell_provider.agent[0].name] : []

  environment = {
    CODER_AGENT_TOKEN = coder_agent.main.token
    CODER_AGENT_URL   = data.coder_workspace.me.access_url
    INIT_SCRIPT       = coder_agent.main.init_script
  }
}
