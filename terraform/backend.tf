terraform {
  backend "s3" {
    # bucket, region and profile come from backend.hcl (see backend.hcl.example)
    key          = "asistente-alfred/terraform.tfstate"
    encrypt      = true
    use_lockfile = true
  }
}
