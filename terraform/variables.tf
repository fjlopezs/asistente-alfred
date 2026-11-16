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
  description = "Skill ID from the Alexa developer console (amzn1.ask.skill...)"
  type        = string
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
  description = "City used for weather when none is said, e.g. \"Santiago, Chile\""
  type        = string
  default     = ""
}

variable "location" {
  description = "Approximate user location for web search (country is ISO 3166-1 alpha-2)"
  type = object({
    city    = optional(string, "")
    region  = optional(string, "")
    country = optional(string, "")
  })
  default = {}
}

variable "lambda_zip" {
  description = "Zip with the Go bootstrap binary, only used when creating the function"
  type        = string
  default     = "../build/alfred.zip"
}

variable "memory_table" {
  type    = string
  default = "alfred-memory"
}

variable "timezone" {
  description = "User's timezone, used to date past conversations"
  type        = string
  default     = "America/Santiago"
}
