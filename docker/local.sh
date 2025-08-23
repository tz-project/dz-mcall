#!/usr/bin/env bash

#ROOTDIR=/Volumes/workspace
#ROOTDIR=/Volumes/workspace/tz/tz-devops-utils/projects
#ROOTDIR=/vagrant/projects

cd $ROOTDIR

mkdir -p $ROOTDIR/go
cd $ROOTDIR/go

curl -OL https://go.dev/dl/go1.18.1.linux-amd64.tar.gz
sha256sum go1.18.1.linux-amd64.tar.gz
sudo tar -C /usr/local -xvf go1.18.1.linux-amd64.tar.gz

vi ~/.bash_profile
export GOROOT=/usr/local/go
#export GOROOT=/usr/local/opt/go/libexec
export GOPATH=$ROOTDIR/go
export PATH=$GOPATH/bin:.:$PATH
source ~/.bash_profile

go version

mkdir bin pkg src
mkdir -p src/github.com
mkdir -p src/github.com/doohee323

cd $GOPATH/src/github.com/doohee323
git clone https://github.com/doohee323/tz-mcall.git
cd tz-mcall

export GO111MODULE=on
#go env -w GO111MODULE=auto
#go mod init github.com/doohee323/tz-mcall
go mod init
go mod tidy
go get ./...
go mod vendor
go get -t github.com/doohee323/tz-mcall

sudo apt update -y
sudo apt install golang-glide -y

glide install
glide update

#glide get github.com/spf13/viper
go get github.com/spf13/viper

#sudo ln -s /usr/local/go/bin/go /usr/local/bin/go
go clean --cache
go build

# Test the refactored application
./mcall -i="ls -al" -f="plain"

exit

sudo chown -Rf vagrant:vagrant /var/run/docker.sock

# Build Docker image
docker build -f docker/Dockerfile -t tz-mcall:latest .

# Run container with webserver enabled
docker run -d -p 3000:3000 --name tz-mcall-container tz-mcall:latest

# Alternative: Run with custom configuration
# docker run -p 3000:3000 -v $(pwd)/etc/mcall.yaml:/app/mcall.yaml -it tz-mcall:latest

# Test the running container
sleep 5
params='{"inputs":[{"input":"ls -al"},{"input":"pwd"}]}'
curl http://localhost:3000/mcall/cmd/`echo $params | base64`

# Health check
curl http://localhost:3000/healthcheck

# Container management
# docker exec -it tz-mcall-container /bin/sh
# docker stop tz-mcall-container
# docker rm tz-mcall-container

# Test with external URL
params='{"inputs":[{"input":"ls -al"},{"input":"pwd"}]}'
curl http://k8s.mcall.tzcorp.com/mcall/cmd/`echo $params | base64`

