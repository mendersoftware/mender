#!/bin/sh

JSON_LIB_DEPENDS_SUBMODULES=" \
    libs/json \
    libs/align \
    libs/assert \
    libs/config \
    libs/container \
    libs/core \
    libs/exception \
    libs/headers \
    libs/intrusive \
    libs/move \
    libs/mp11 \
    libs/static_assert \
    libs/system \
    libs/throw_exception \
"

echo "==== Bootstrapping boost ===="
(
    cd "$(dirname "$0")/vendor/boost"
    git submodule init
    git submodule update tools/build tools/boost_install
    git submodule update $JSON_LIB_DEPENDS_SUBMODULES
    ./bootstrap.sh
)
