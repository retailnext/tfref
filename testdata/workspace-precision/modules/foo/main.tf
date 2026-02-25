resource "aws_s3_bucket" "data" {
  bucket = "my-data-bucket"
}

output "result" {
  value = aws_s3_bucket.data.id
}
