variable "foo_result" {}

resource "aws_s3_bucket_versioning" "versioning" {
  bucket = var.foo_result  # depends on foo_result → foo
}

# derived_output: depends on foo (via var.foo_result → versioning)
output "derived_output" {
  value = aws_s3_bucket_versioning.versioning.id
}

# independent_output: does NOT depend on foo at all
output "independent_output" {
  value = "static-value"
}
