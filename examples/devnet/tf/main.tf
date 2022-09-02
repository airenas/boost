terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 4.0"
    }
  }
  required_version = ">= 0.14.9"
}

provider "aws" {
  region  = "eu-central-1"
  profile = "aireno"
}

resource "aws_security_group" "boost_sg" {
  name = "${var.pr_name}-sg"
  ingress {
    from_port   = 8000
    to_port     = 8000
    protocol    = "tcp"
    cidr_blocks = ["78.58.39.6/32"]
  }

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["78.58.39.6/32"]
    description = "aireno ssh"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_instance" "on_demand_boost" {
  instance_type = "a1.xlarge"
  key_name      = "key02"
  ami = "ami-05ca7bb915b326313"
  vpc_security_group_ids = [aws_security_group.boost_sg.id]
  tags = {
    Name = "On demand: ${var.pr_name}"
  }
  ebs_block_device {
    device_name = "/dev/sda1"
    volume_size = 30
  }
}

# resource "aws_eip_association" "eip_assoc" {
#   instance_id   = aws_instance.on_demand_boost.id
#   allocation_id = var.eip_id
# }
