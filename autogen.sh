#!/bin/sh
set -e

if [ ! -f "$(dirname "$0")/vendor/googletest/README.md" ] ; then
    echo "Please run the following to clone submodules first:"
    echo "  git submodule update --init"
    exit 1
fi

if [ ! -f "$(dirname "$0")/vendor/boost/b2" ] ; then
    $(dirname "$0")/bootstrap_boost.sh
fi

cd "$(dirname "$0")"
aclocal --install
autoreconf --install

