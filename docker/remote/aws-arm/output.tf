output "public_ip" {
  description = "Public IP address of the instance"
  value       = aws_instance.on_demand_boost.public_ip
}

output "instance_id" {
  description = "ID of the instance"
  value       = aws_instance.on_demand_boost.id
}