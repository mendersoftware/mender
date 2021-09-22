#/bin/sh

set -e

if [ -n "$SERVER_URL" ]; then
    sed -i -e "s#\"ServerURL\": *\"[^\"]*\"#\"ServerURL\": \"$SERVER_URL\"#" /etc/mender/mender.conf
fi
if [ -n "$TENANT_TOKEN" ]; then
    sed -i -e "s/\"TenantToken\": *\"[^\"]*\"/\"TenantToken\": \"$TENANT_TOKEN\"/" /etc/mender/mender.conf
fi

/etc/init.d/ssh start
mender daemon
