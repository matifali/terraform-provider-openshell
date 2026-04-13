data "openshell_provider" "anthropic" {
  name = "anthropic"
}

output "provider_type" {
  value = data.openshell_provider.anthropic.type
}
