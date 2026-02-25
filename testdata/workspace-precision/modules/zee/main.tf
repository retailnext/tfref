variable "derived_data" {}

resource "aws_cloudwatch_metric_alarm" "alarm" {
  alarm_name          = "derived-alarm"
  comparison_operator = "GreaterThanThreshold"
  dimensions = {
    BucketName = var.derived_data
  }
  evaluation_periods  = 1
  metric_name         = "NumberOfObjects"
  namespace           = "AWS/S3"
  period              = 86400
  statistic           = "Average"
  threshold           = 1000
}

output "final_output" {
  value = aws_cloudwatch_metric_alarm.alarm.id
}
