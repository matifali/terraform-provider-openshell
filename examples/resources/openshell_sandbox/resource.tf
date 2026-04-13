resource "openshell_sandbox" "claude" {
  name  = "my-claude-sandbox"
  image = "ghcr.io/nvidia/openshell/sandbox:latest"

  providers = ["anthropic"]

  environment = {
    WORKSPACE = "/home/user/project"
  }

  gpu = false
}
