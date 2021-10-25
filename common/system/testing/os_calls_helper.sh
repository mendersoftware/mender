#!/bin/sh

# Drain input if there is any.
cat > /dev/null

# Check if we have something to print
if [ -n "$2" ]; then
    printf "%s\n" "$2"
fi

# Helper process return code
exit "$1"
