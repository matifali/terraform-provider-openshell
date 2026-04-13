data "openshell_providers" "all" {}

output "provider_names" {
  value = [for p in data.openshell_providers.all.providers : p.name]
}
