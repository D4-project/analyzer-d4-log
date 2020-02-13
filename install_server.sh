#!/bin/bash

set -e
set -x

sudo apt-get install screen -y

go get github.com/D4-project/analyzer-d4-log

# REDIS #
mkdir -p db
test ! -d redis/ && git clone https://github.com/antirez/redis.git
pushd redis/
git checkout 5.0
make
popd
