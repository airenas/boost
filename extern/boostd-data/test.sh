#!/bin/bash
function run(){
    echo Run clean
    docker compose down
    echo Run tests...
    docker compose up --build --exit-code-from go-tests
}

for i in {1..20}; do echo "start test: $i"; run || exit 1; done
