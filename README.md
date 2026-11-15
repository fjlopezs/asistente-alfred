# Asistente Alfred

Personal Alexa custom skill (invocation: "asistente alfred") that answers questions with Claude, in Spanish, with a deep male voice (Polly "Miguel").

## Layout

- `code/` — Lambda handler (Go, `provided.al2023`, arm64). Claude via `anthropic-sdk-go`, weather tool backed by [Open-Meteo](https://open-meteo.com/) and Claude web search for current information.
- `terraform/` — AWS resources (IAM role, Lambda, permission for Alexa, log group, DynamoDB memory table). Terraform 1.17.2, AWS provider 6.73.
- `interaction-model.es-MX.json`, `interaction-model.es-US.json` — Alexa interaction models (es-US is synced from es-MX).

## Configuration (Lambda env vars)

| Variable | Purpose |
|---|---|
| `CLAUDE_MODEL` | Model ID (default `claude-haiku-5-5`) |
| `API_KEY_PARAM` | SSM SecureString holding the Anthropic API key |
| `ALEXA_SKILL_ID` | Rejects requests from other skills |
| `DEFAULT_CITY` | City used for weather when none is said |
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

## Infrastructure

```sh
cd terraform && terraform init   # once
make plan
make apply
```

Terraform manages the function configuration; code is deployed with the command above (`filename`/`source_code_hash` are ignored). The API key SSM parameter is created by hand and not managed, so the key never lands in state. State lives in S3: `s3://infra-123456789012/asistente-alfred/terraform.tfstate` (private, versioned, encrypted, native S3 locking). The bucket was created by hand.

Interaction model changes are applied in the Alexa developer console (JSON Editor → Save → Build).
