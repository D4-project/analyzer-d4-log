#!/bin/bash

set -e
set -x

sudo apt-get install screen golang -y
go get -u
go build

# REDIS #
mkdir -p db
test ! -d redis/ && git clone https://github.com/antirez/redis.git
pushd redis/
git checkout 5.0
make
popd
