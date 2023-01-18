-include Makefile.options
server=$(shell terraform output -raw public_ip )
SSH_CMD=ssh -i $(pem) -t $(user)@$(server)
RSYNC_CMD=rsync -e "ssh -i $(pem)"
##############################################################################################
ssh:
	$(SSH_CMD)
ssh/tunel:	
	$(SSH_CMD) -L 5900:localhost:5900
##############################################################################################
.terraform:
	terraform init

tf/destroy: 
	terraform destroy

tf/create: .terraform
	terraform apply

