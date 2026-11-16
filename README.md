<p align="center"><img src="docs/banner.svg" alt="Asistente Alfred: mayordomo de voz para Alexa, con respuestas de Claude" width="100%"></p>

# Asistente Alfred

[![Unit tests](https://github.com/fjlopezs/asistente-alfred/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/fjlopezs/asistente-alfred/actions/workflows/test.yml)
[![Coverage](https://github.com/fjlopezs/asistente-alfred/raw/badges/coverage.svg)](https://github.com/fjlopezs/asistente-alfred/actions/workflows/test.yml)

Personal Alexa custom skill (invocation: "asistente alfred") that answers questions with Claude, in Spanish, with a deep male voice (Polly "Miguel").

## Layout

- `code/` — Lambda handler (Go, `provided.al2023`, arm64). Claude via `anthropic-sdk-go`, weather tool backed by [Open-Meteo](https://open-meteo.com/) and Claude web search for current information.
- `terraform/` — AWS resources (IAM role, Lambda, permission for Alexa, log group, DynamoDB memory table). Terraform 1.17.2, AWS provider 6.73.
- `interaction-model.es-MX.json`, `interaction-model.es-US.json` — Alexa interaction models (es-US is synced from es-MX).

## Setup

1. Create a custom skill in the [Alexa developer console](https://developer.amazon.com/alexa/console/ask) (locale es-MX, optionally es-US) and paste `interaction-model.es-MX.json` in the JSON Editor.
2. Store an [Anthropic API key](https://console.anthropic.com/) as an SSM SecureString:
   `aws ssm put-parameter --name /alfred/anthropic-api-key --type SecureString --value "$KEY"`
3. Fill `terraform/backend.hcl` and `terraform/terraform.tfvars` (see [Infrastructure](#infrastructure)), then `make zip init apply`.
4. Point the skill's endpoint to the Lambda ARN (`terraform output`) and build the model.

## Configuration (Lambda env vars)

| Variable | Purpose |
|---|---|
| `CLAUDE_MODEL` | Model ID (default `claude-haiku-5-5`) |
| `API_KEY_PARAM` | SSM SecureString holding the Anthropic API key |
| `ALEXA_SKILL_ID` | Rejects requests from other skills |
| `DEFAULT_CITY` | City used for weather when none is said |
| `LOCATION_CITY`, `LOCATION_REGION`, `LOCATION_COUNTRY` | Approximate location to localize web search (optional) |
| `MEMORY_TABLE` | DynamoDB table for conversation memory (unset disables it) |
| `TIMEZONE` | User's timezone, used to date past conversations |

## Memory

Each question/answer is stored in DynamoDB (`alfred-memory`, key `user` + `ts`) with a 7-day TTL (`expires_at`). On every question, up to 20 Q/A pairs from previous sessions of the same Alexa user are added to the system prompt; the current session travels in the session attributes. Memory errors are logged and never fail a response.

## Build and deploy

```sh
make test     # go vet + go test
make deploy   # test, build linux/arm64, zip and upload to Lambda
make logs     # tail CloudWatch logs (SINCE=1h to go further back)
make help     # all targets
```

Defaults: `AWS_PROFILE=personal`, `AWS_REGION=us-east-1`, `FUNCTION=alfred-alexa-skill` (override on the command line).

## CI

`.github/workflows/test.yml` runs on every push and pull request: `gofmt` check, `go vet`, and `go test -race -shuffle=on` with coverage (per-function report in the job summary). On `main`, it renders the coverage badge and pushes it to the orphan `badges` branch, so the README needs no third-party service.

## Infrastructure

```sh
cp terraform/backend.hcl.example terraform/backend.hcl          # state bucket, region, profile
cp terraform/terraform.tfvars.example terraform/terraform.tfvars # skill ID, city, location
make init    # once
make plan
make apply
```

Both copies are gitignored. Terraform manages the function configuration; code is deployed with `make deploy` (`filename`/`source_code_hash` are ignored). The API key SSM parameter is created by hand and not managed, so the key never lands in state. State lives in an S3 bucket you create beforehand (versioned, encrypted, native S3 locking).

Interaction model changes are applied in the Alexa developer console (JSON Editor → Save → Build).

## License

[MIT](LICENSE)
