-include Makefile.options
server=$(shell terraform output -raw public_ip )
SSH_CMD=ssh -i $(pem) -t $(user)@$(server)
RSYNC_CMD=rsync -e "ssh -i $(pem)"
##############################################################################################
ssh:
	$(SSH_CMD)
##############################################################################################
.terraform:
	terraform init

tf/destroy: 
	terraform destroy

tf/create: .terraform
	terraform apply

