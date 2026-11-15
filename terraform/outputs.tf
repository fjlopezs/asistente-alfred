output "lambda_arn" {
  description = "Endpoint to set in the Alexa developer console"
  value       = aws_lambda_function.alfred.arn
}
