terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 4.31"
    }
  }
  required_version = ">= 0.14.9"
}

provider "aws" {
  region  = var.aws_region
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
    from_port   = 8080
    to_port     = 8080
    protocol    = "tcp"
    cidr_blocks = ["78.58.39.6/32"]
  }

  ingress {
    from_port   = 5900
    to_port     = 5900
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

resource "aws_ec2_host" "macos-host" {
  instance_type     = "mac2.metal"
  availability_zone = var.aws_region_az
  tags = {
    Name = "macos-host"
  }
}

resource "aws_instance" "dedicated_mac" {
  host_id       = aws_ec2_host.macos-host.id
  instance_type = "mac2.metal"
  key_name      = "key-oh-01"
  ami = "ami-0dc9d5c881eafe57e"
  vpc_security_group_ids = [aws_security_group.boost_sg.id]
  tags = {
    Name = "Dedicated: ${var.pr_name}"
  }
  ebs_block_device {
    device_name = "/dev/sda1"
    volume_size = 200
  }
}

# resource "aws_eip_association" "eip_assoc" {
#   instance_id   = aws_instance.on_demand_boost.id
#   allocation_id = var.eip_id
# }
