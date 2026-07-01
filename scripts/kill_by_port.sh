#!/bin/bash

# usage:
# ./kill_by_port.sh <port_number>

# runs lsof -i :<port_number> and kills the processes using that port

if [ -z "$1" ]; then
  echo "Usage: $0 <port_number>"
  exit 1
fi

PORT=$1

PIDS=$(lsof -ti :"$PORT")

if [ -z "$PIDS" ]; then
  echo "No processes found on port $PORT"
  exit 0
fi

echo "Killing processes on port $PORT: $PIDS"
echo "$PIDS" | xargs kill -9
echo "Done"