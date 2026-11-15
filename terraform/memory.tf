# Conversation memory: one item per question/answer, expired by DynamoDB TTL.
# Encrypted at rest with the default AWS-owned key.
resource "aws_dynamodb_table" "memory" {
  name         = var.memory_table
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "user"
  range_key    = "ts"

  attribute {
    name = "user"
    type = "S"
  }

  attribute {
    name = "ts"
    type = "N"
  }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }
}

resource "aws_iam_role_policy" "memory" {
  name = "dynamodb-memory"
  role = aws_iam_role.lambda.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["dynamodb:Query", "dynamodb:PutItem"]
      Resource = aws_dynamodb_table.memory.arn
    }]
  })
}
