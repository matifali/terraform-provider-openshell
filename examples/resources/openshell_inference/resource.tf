resource "openshell_inference" "default" {
  provider_name = "openai"
  model_id      = "gpt-4o"
  route_name    = "inference.local"
  timeout_secs  = 120
}
