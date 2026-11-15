variable "region" {
  type    = string
  default = "us-east-1"
}

variable "aws_profile" {
  type    = string
  default = "personal"
}

variable "function_name" {
  type    = string
  default = "alfred-alexa-skill"
}

variable "alexa_skill_id" {
  type    = string
  default = "amzn1.ask.skill.00000000-0000-0000-0000-000000000000"
}

variable "claude_model" {
  type    = string
  default = "claude-haiku-5-5"
}

# SecureString created by hand; not managed here so the key never lands in state.
variable "api_key_param" {
  type    = string
  default = "/alfred/anthropic-api-key"
}

variable "default_city" {
  type    = string
  default = "Temuco, Chile"
}

variable "lambda_zip" {
  description = "Zip with the Go bootstrap binary, only used when creating the function"
  type        = string
  default     = "../build/alfred-go.zip"
}
