# Creates a container which acts as a bare bones non-VM based Mender
# installation, for use in tests.
FROM ubuntu:18.04 AS build

RUN apt update
RUN apt install -y make git build-essential golang liblzma-dev jq

COPY ./ /go/src/github.com/mendersoftware/mender/
RUN make -C /go/src/github.com/mendersoftware/mender GOPATH=/go clean
RUN make -C /go/src/github.com/mendersoftware/mender GOPATH=/go prefix=/mender-install install
RUN sh -c 'jq ".ServerCertificate=\"/etc/mender/server.crt\" | .ServerURL=\"https://docker.mender.io/\"" < /mender-install/etc/mender/mender.conf.demo > /mender-install/etc/mender/mender.conf'

FROM ubuntu:18.04

RUN apt update
RUN apt install -y openssh-server

# Set no password
RUN sed -ie 's/^root:[^:]*:/root::/' /etc/shadow
RUN sed -ie 's/^UsePAM/#UsePam/' /etc/ssh/sshd_config
RUN sh -c 'echo PermitEmptyPasswords yes >> /etc/ssh/sshd_config'
RUN sh -c 'echo PermitRootLogin yes >> /etc/ssh/sshd_config'

RUN sh -c 'echo Port 22 >> /etc/ssh/sshd_config'
RUN sh -c 'echo Port 8822 >> /etc/ssh/sshd_config'

COPY --from=build /mender-install/ /
COPY support/demo.crt /etc/mender/server.crt

RUN sh -c 'mkdir -p /var/lib/mender && echo device_type=docker-client > /var/lib/mender/device_type'
RUN sh -c 'echo artifact_name=original > /etc/mender/artifact_info'

CMD /etc/init.d/ssh start && mender -daemon
