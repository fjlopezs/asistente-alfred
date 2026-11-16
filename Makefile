AWS_PROFILE ?= personal
AWS_REGION  ?= us-east-1
FUNCTION    ?= alfred-alexa-skill
SINCE       ?= 15m

AWS := aws --profile $(AWS_PROFILE) --region $(AWS_REGION)
ZIP := build/alfred.zip
BIN := build/bootstrap

.PHONY: help test build zip deploy logs init plan apply fmt clean

help: ## List targets
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-8s %s\n", $$1, $$2}'

test: ## Run Go tests
	cd code && go vet ./... && go test ./...

build: ## Build the Lambda binary (linux/arm64)
	@mkdir -p build
	cd code && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -trimpath -ldflags="-s -w" -o ../$(BIN) .

zip: build ## Package the binary
	rm -f $(ZIP) && zip -qj $(ZIP) $(BIN)

deploy: test zip ## Test, build and upload the code to Lambda
	$(AWS) lambda update-function-code --function-name $(FUNCTION) --zip-file fileb://$(ZIP) --query '[FunctionName,LastUpdateStatus,CodeSize]' --output text
	$(AWS) lambda wait function-updated --function-name $(FUNCTION)

logs: ## Tail Lambda logs (SINCE=15m)
	$(AWS) logs tail /aws/lambda/$(FUNCTION) --since $(SINCE) --format short --follow

init: ## Terraform init (needs terraform/backend.hcl)
	cd terraform && terraform init -backend-config=backend.hcl

plan: ## Terraform plan
	cd terraform && terraform plan

apply: ## Terraform apply
	cd terraform && terraform apply

fmt: ## Format Go and Terraform
	cd code && gofmt -w .
	cd terraform && terraform fmt

clean: ## Remove build output
	rm -rf build
