# Mac M1 on AWS

## init

```bash
terraform apply
```

## connect to mac

https://aws.amazon.com/premiumsupport/knowledge-center/ec2-mac-instance-gui-access/

```bash
# on remote mac
sudo defaults write /var/db/launchd.db/com.apple.launchd/overrides.plist com.apple.screensharing -dict Disabled -bool false
sudo launchctl load -w /System/Library/LaunchDaemons/com.apple.screensharing.plist

sudo /usr/bin/dscl . -passwd /Users/ec2-user
```

```bash
# on local
make ssh/tunel
```

## do on Mac

```bash
brew update
brew install htop

git clone https://github.com/airenas/boost/
cd boost
```