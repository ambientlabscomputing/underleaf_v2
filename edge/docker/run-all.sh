#!/bin/sh

# start the orchestrator server
/usr/local/bin/orch-server run &

# start nginx
nginx -g 'daemon off;'
