#!/bin/bash
rsync -rtv --delete \
    --exclude='.git/' \
    --exclude='build/' \
    --exclude='docker/linkding-data/' \
    --exclude='update.sh' \
    . linkding.igo:~/src/bookmark-sync/
