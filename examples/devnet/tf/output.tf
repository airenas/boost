output "public_ip" {
  description = "Public IP address of the instance"
  value       = aws_instance.dedicated_mac.public_ip
}

output "instance_id" {
  description = "ID of the instance"
  value       = aws_instance.dedicated_mac.id
}