#!/bin/sh
echo "============================================"
echo " Docksmith Sample App"
echo "============================================"
echo "Hostname : $(hostname)"
echo "Working  : $(pwd)"
echo "Greeting : $GREETING"
echo "Author   : $AUTHOR"
echo "============================================"
echo "Filesystem root contents:"
ls /
echo "============================================"
echo "App ran successfully inside isolated container"
# changed
# v2
