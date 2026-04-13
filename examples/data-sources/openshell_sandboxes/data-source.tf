data "openshell_sandboxes" "all" {}

output "sandbox_names" {
  value = [for s in data.openshell_sandboxes.all.sandboxes : s.name]
}
