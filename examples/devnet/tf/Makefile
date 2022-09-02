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


install:
	curl -fsSL https://get.docker.com -o get-docker.sh
	sudo sh get-docker.sh	
	sudo usermod -aG docker $USER
	newgrp docker
	git clone https://github.com/airenas/boost.git
	sudo apt install make
	cd boost
	git checkout docker-files-arm
	curl -OL https://go.dev/dl/go1.18.5.linux-arm64.tar.gz
	rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.18.5.linux-arm64.tar.gz
	curl -OL https://github.com/filecoin-project/filecoin-ffi/releases/download/f997fe6c77632c0b/filecoin-ffi-Darwin-standard.tar.gz