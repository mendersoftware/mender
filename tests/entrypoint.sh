#/bin/sh

set -e

if [ -n "$TENANT_TOKEN" ]; then
    sed -i -e "s/\"TenantToken\": *\"[^\"]*\"/\"TenantToken\": \"$TENANT_TOKEN\"/" /etc/mender/mender.conf
fi

/etc/init.d/ssh start
mender daemon
