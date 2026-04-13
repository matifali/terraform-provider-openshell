resource "openshell_provider" "anthropic" {
  name = "anthropic"
  type = "claude"

  credentials = {
    ANTHROPIC_API_KEY = var.anthropic_api_key
  }
}
