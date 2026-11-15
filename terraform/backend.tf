terraform {
  backend "s3" {
    bucket       = "infra-123456789012"
    key          = "asistente-alfred/terraform.tfstate"
    region       = "us-east-1"
    profile      = "personal"
    encrypt      = true
    use_lockfile = true
  }
}
