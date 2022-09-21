variable "pr_name" {
  default = "boost"
}

variable "aws_region_az" {
  type        = string
  description = "AWS Region AZ"
  default     = "eu-west-1b"
}

variable "aws_region" {
  type        = string
  description = "AWS Region"
  default     = "eu-west-1"
}