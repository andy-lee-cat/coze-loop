#!/bin/sh
export no_proxy="localhost,127.0.0.1"

HOST="localhost"
PORT="8888"
BASE_URL="http://${HOST}:${PORT}"

curl -s "${BASE_URL}/ping" | grep -q pong