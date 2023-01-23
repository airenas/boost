data "template_file" "init_instance" {
  template = <<EOF
#!/bin/bash -xe
pwd > /home/ubuntu/.install
apt-get update -y &>> /home/ubuntu/.install
apt-get install -y mc htop jq curl make git &>> /home/ubuntu/.install
echo installed >> /home/ubuntu/.install

curl -fsSL https://get.docker.com -o /home/ubuntu/get-docker.sh &>> /home/ubuntu/.install
sh get-docker.sh &>> /home/ubuntu/.install
sudo usermod -aG docker ubuntu &>> /home/ubuntu/.install
echo installed docker >> /home/ubuntu/.install 
rm -f /home/ubuntu/get-docker.sh

git clone https://github.com/filecoin-project/boost.git
echo Done &>> /home/ubuntu/.install
EOF
}
