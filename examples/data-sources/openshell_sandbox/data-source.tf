data "openshell_sandbox" "example" {
  name = "my-claude-sandbox"
}

output "sandbox_phase" {
  value = data.openshell_sandbox.example.phase
}
