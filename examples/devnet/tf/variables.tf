variable "pr_name" {
  default = "boost"
}

variable "aws_region_az" {
  type        = string
  description = "AWS Region AZ"
  default     = "us-east-2c"
}

variable "aws_region" {
  type        = string
  description = "AWS Region"
  default     = "us-east-2"
}