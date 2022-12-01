#!/bin/sh

if [ ! -f "$(dirname "$0")/vendor/googletest/README.md" ] ; then
    echo "Please run the following to clone submodules first:"
    echo "  git submodule update --init"
    exit 1
fi

if [ ! -f "$(dirname "$0")/vendor/boost/libs/json/README.md" ] ; then
    echo "Please initialize and build required Boost libraries with:"
    echo "  ./bootstrap_boost.sh"
    exit 1
fi

( cd "$(dirname "$0")" && autoreconf -i )
