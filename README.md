# Asistente Alfred

Personal Alexa custom skill (invocation: "asistente alfred") that answers questions with Claude, in Spanish, with a deep male voice (Polly "Miguel").

## Layout

- `lambda-go/` — Lambda handler (Go, `provided.al2023`, arm64). Claude via `anthropic-sdk-go`, weather tool backed by [Open-Meteo](https://open-meteo.com/).
- `interaction-model.es-MX.json`, `interaction-model.es-US.json` — Alexa interaction models (es-US is synced from es-MX).
- `lambda/` — original Python version, kept for reference.

## Configuration (Lambda env vars)

| Variable | Purpose |
|---|---|
| `CLAUDE_MODEL` | Model ID (default `claude-haiku-5-5`) |
| `API_KEY_PARAM` | SSM SecureString holding the Anthropic API key |
| `ALEXA_SKILL_ID` | Rejects requests from other skills |
| `DEFAULT_CITY` | City used for weather when none is said |

## Build and deploy

```sh
cd lambda-go
go test ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -trimpath -ldflags="-s -w" -o ../build/go/bootstrap .
cd ../build/go && zip ../alfred-go.zip bootstrap
aws lambda update-function-code --function-name alfred-alexa-skill --zip-file fileb://../alfred-go.zip
```

Interaction model changes are applied in the Alexa developer console (JSON Editor → Save → Build).
