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


RES=0
if [ -e $TEST.parted ]; then
    ./_parted.sh > _tmp
    echo -n "PARTED: "
    if diff --ignore-all-space $TEST.parted _tmp; then
	echo "OK"
    else
	RES=1
    fi
fi

if [ -e $TEST.df ]; then
    ./_df.sh > _tmp
    echo -n "DF: "
    if diff --ignore-all-space $TEST.df _tmp; then
	echo "OK"
    else
	RES=1
    fi
fi

if [ `cat /mnt/test/ok`"" != "OK" ]; then
    echo "LOST ok-file"
    RES=1
fi

[ "$RES" == "0" ] && echo "OK"
rm ./_tmp
exit $RES

