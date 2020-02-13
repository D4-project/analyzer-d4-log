#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
CONF="conf.sample"

screen -dmS "alog"
sleep 0.1

screen -S "alog" -X screen -t "alog-redis" bash -c "(${DIR}/redis/src/redis-server ${DIR}/redis.conf); read x;"
screen -S "alog" -X screen -t "alog-ingester" bash -c "(${DIR}/analyzer-d4-log -c ${CONF}); read x;"

exit 0
