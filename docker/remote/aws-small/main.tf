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

resource "aws_security_group" "boost_sg_2" {
  name = "${var.pr_name}-sg-2"
  ingress {
    from_port   = 8000
    to_port     = 8000
    protocol    = "tcp"
    cidr_blocks = ["${var.dev_ip}/32"]
  }
  ingress {
    from_port   = 8080
    to_port     = 8080
    protocol    = "tcp"
    cidr_blocks = ["${var.dev_ip}/32"]
  }

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["${var.dev_ip}/32"]
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
  instance_type = "t2.small"
  key_name      = "key02"
  ami = "ami-0502e817a62226e03"
  vpc_security_group_ids = [aws_security_group.boost_sg_2.id]
  tags = {
    Name = "On demand: ${var.pr_name}"
  }
  ebs_block_device {
    device_name = "/dev/sda1"
    volume_size = 50
  }
  user_data = base64encode(data.template_file.init_instance.rendered)
}

