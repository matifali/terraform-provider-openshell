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
# Template: Coder Agents sandbox
#
# This template is for Coder's built-in AI agents (chatd). The LLM runs
# in the Coder control plane — not inside the workspace. The workspace
# is just the execution environment where tool calls (execute, read_file,
# write_file, etc.) land via the coder_agent.
#
# No API keys are needed inside the sandbox because the control plane
# holds the LLM credentials. The sandbox only needs:
#   - Network access back to the Coder server (for the agent to phone home)
#   - A writable home directory (for tool calls to read/write files)
#   - Standard dev tools (git, ssh, etc.)
#
# OpenShell adds defense-in-depth for tool call execution:
#   - Filesystem isolation — tool calls can't read outside /home/coder
#   - Network allow-list — tool calls can't reach arbitrary endpoints
#   - Process restrictions — no privilege escalation
#
# This is the `-coderd-chat` agent pattern. chatd auto-discovers this
# agent by the name suffix and routes chat sessions to it.
# ---------------------------------------------------------------------------

locals {
  username = data.coder_workspace_owner.me.name
  # The Coder access URL, rewritten for container networking.
  coder_url = replace(
    data.coder_workspace.me.access_url,
    "/localhost|127\\.0\\.0\\.1/",
    "host.docker.internal"
  )
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

# ---------------------------------------------------------------------------
# Provider configuration
# ---------------------------------------------------------------------------

provider "openshell" {
  gateway_url = data.coder_parameter.gateway_url.value
}

data "coder_provisioner" "me" {}
data "coder_workspace" "me" {}
data "coder_workspace_owner" "me" {}

# ---------------------------------------------------------------------------
# Agent 1: Developer agent (user-facing, appears in dashboard)
# ---------------------------------------------------------------------------

resource "coder_agent" "dev" {
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
  agent_id = coder_agent.dev.id
  order    = 1
}

module "jetbrains" {
  count      = data.coder_workspace.me.start_count
  source     = "registry.coder.com/coder/jetbrains/coder"
  version    = "~> 1.1"
  agent_id   = coder_agent.dev.id
  agent_name = "dev"
  folder     = "/home/coder"
}

# ---------------------------------------------------------------------------
# Agent 2: Chat agent (for chatd-managed AI sessions)
#
# The `-coderd-chat` suffix tells chatd to route chat sessions here.
# The LLM runs in the Coder control plane. This agent only receives
# tool calls (execute, read_file, write_file) — it does NOT need
# API keys or LLM credentials.
#
# Running this agent inside an OpenShell sandbox means every tool call
# executes in an isolated environment with:
#   - Landlock filesystem isolation
#   - Network policy enforcement
#   - Process restrictions (no privilege escalation)
# ---------------------------------------------------------------------------

locals {
  chat_agent_init  = replace(coder_agent.dev-coderd-chat.init_script, "/localhost|127\\.0\\.0\\.1/", "host.docker.internal")
  chat_agent_token = coder_agent.dev-coderd-chat.token
}

resource "coder_agent" "dev-coderd-chat" {
  arch           = data.coder_provisioner.me.arch
  os             = "linux"
  order          = 99
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
}

# ---------------------------------------------------------------------------
# Sandbox 1: Developer workspace (regular agent, no special isolation)
# ---------------------------------------------------------------------------

resource "openshell_sandbox" "dev" {
  count = data.coder_workspace.me.start_count

  name  = "coder-${local.username}-${lower(data.coder_workspace.me.name)}"
  image = data.coder_parameter.image.value

  # No credential providers needed — this is the developer's workspace.
  providers = []

  environment = {
    CODER_AGENT_TOKEN = coder_agent.dev.token
    CODER_AGENT_URL   = data.coder_workspace.me.access_url
    INIT_SCRIPT       = coder_agent.dev.init_script
  }
}

# ---------------------------------------------------------------------------
# Sandbox 2: Chat agent sandbox (OpenShell-isolated tool execution)
#
# This replaces docker-chat-sandbox's bubblewrap container. OpenShell
# provides stronger isolation:
#
#   bubblewrap                          OpenShell
#   ──────────────────────────────────  ─────────────────────────────────
#   Read-only root filesystem           Landlock filesystem policy
#   iptables OUTPUT rules               OPA network policy (per-binary)
#   No credential management            Credential proxy (placeholder keys)
#   No inference routing                inference.local → self-hosted LLM
#   No audit trail                      OCSF structured audit logging
#   Requires SYS_ADMIN + NET_ADMIN      Runs unprivileged
#
# No API keys are injected. The LLM runs in the Coder control plane.
# The sandbox only needs network access to the Coder server so the
# agent can phone home, and a writable /home/coder for tool calls.
# ---------------------------------------------------------------------------

resource "openshell_sandbox" "chat" {
  count = data.coder_workspace.me.start_count

  name  = "coder-${local.username}-${lower(data.coder_workspace.me.name)}-chat"
  image = data.coder_parameter.image.value

  # No credential providers — the LLM runs in the control plane,
  # not inside this sandbox.
  providers = []

  environment = {
    CODER_AGENT_TOKEN = local.chat_agent_token
    CODER_AGENT_URL   = local.coder_url
    INIT_SCRIPT       = local.chat_agent_init
  }
}
