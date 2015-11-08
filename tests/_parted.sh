#!/bin/bash

parted -m /dev/sdb unit GB print free | tail -n +3 | sed s/primary//
