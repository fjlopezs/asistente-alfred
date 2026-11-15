data "aws_caller_identity" "current" {}

locals {
  api_key_param_arn = "arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter${var.api_key_param}"
}

resource "aws_iam_role" "lambda" {
  name = "alfred-alexa-lambda"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "basic_execution" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "ssm_api_key" {
  name = "ssm-api-key"
  role = aws_iam_role.lambda.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "ssm:GetParameter"
      Resource = local.api_key_param_arn
    }]
  })
}

resource "aws_cloudwatch_log_group" "lambda" {
  name = "/aws/lambda/${var.function_name}"
}

resource "aws_lambda_function" "alfred" {
  function_name = var.function_name
  role          = aws_iam_role.lambda.arn
  runtime       = "provided.al2023"
  handler       = "bootstrap"
  architectures = ["arm64"]
  memory_size   = 256
  timeout       = 10 # Alexa gives up after ~8s
  filename      = var.lambda_zip

  environment {
    variables = {
      CLAUDE_MODEL   = var.claude_model
      API_KEY_PARAM  = var.api_key_param
      ALEXA_SKILL_ID = var.alexa_skill_id
      DEFAULT_CITY   = var.default_city
    }
  }

  # Code is deployed with `aws lambda update-function-code` (see README).
  lifecycle {
    ignore_changes = [filename, source_code_hash]
  }

  depends_on = [aws_cloudwatch_log_group.lambda, aws_iam_role_policy_attachment.basic_execution]
}

resource "aws_lambda_permission" "alexa" {
  statement_id       = "alexa-skill-alfred"
  action             = "lambda:InvokeFunction"
  function_name      = aws_lambda_function.alfred.function_name
  principal          = "alexa-appkit.amazon.com"
  event_source_token = var.alexa_skill_id
}
