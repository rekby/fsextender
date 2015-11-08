#!/bin/bash

TEST="$1"

if [ "$TEST" == "" ]; then
    echo "Usage: $0 <test-name>[.[sh]]"
    exit 1
fi

TEST="${TEST%.}"
TEST="${TEST%.sh}"
[ -e $TEST.sh ] || echo "Doesn't exist test: $TEST.sh"
[ -e $TEST.sh ] || exit 1

./$TEST.sh
fsextender /mnt/test --do
./_check.sh $TEST.sh
