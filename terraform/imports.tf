# Resources created by hand before OpenTofu. Safe to keep: no-ops once in state.

import {
  to = aws_iam_role.lambda
  id = "alfred-alexa-lambda"
}

import {
  to = aws_iam_role_policy_attachment.basic_execution
  id = "alfred-alexa-lambda/arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

import {
  to = aws_iam_role_policy.ssm_api_key
  id = "alfred-alexa-lambda:ssm-api-key"
}

import {
  to = aws_cloudwatch_log_group.lambda
  id = "/aws/lambda/alfred-alexa-skill"
}

import {
  to = aws_lambda_function.alfred
  id = "alfred-alexa-skill"
}

import {
  to = aws_lambda_permission.alexa
  id = "alfred-alexa-skill/alexa-skill-alfred"
}
