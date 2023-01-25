module github.com/mendersoftware/mender

go 1.14

require (
	github.com/Azure/go-ansiterm v0.0.0-20170929234023-d6e3b3328b78 // indirect
	github.com/Microsoft/hcsshim v0.8.9 // indirect
	github.com/aws/aws-sdk-go v1.30.27 // indirect
	github.com/bmatsuo/lmdb-go v1.6.1-0.20160816100615-69ad631904c9
	github.com/containerd/containerd v1.3.4 // indirect
	github.com/containerd/continuity v0.0.0-20200709052629-daa8e1ccc0bc // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v1.4.2-0.20200319182547-c7ad2b866182 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/godbus/dbus v4.1.0+incompatible
	github.com/gorilla/mux v1.7.4 // indirect
	github.com/gorilla/websocket v1.4.3-0.20220104015952-9111bb834a68
	github.com/hashicorp/go-kms-wrapping/entropy v0.1.0 // indirect
	github.com/mendersoftware/mender-artifact v0.0.0-20230125055725-c322771c6a2c
	github.com/mendersoftware/openssl v1.1.1-0.20221101131127-8797d18baf1a
	github.com/mendersoftware/progressbar v0.0.3
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/opencontainers/runc v0.1.1 // indirect
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.0
	github.com/stretchr/testify v1.8.1
	github.com/ungerik/go-sysfs v0.0.0-20190613143942-7f098ddb67a6
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/sys v0.0.0-20220728004956-3c1f35247d10
	golang.org/x/term v0.0.0-20210927222741-03fcf44c2211
	gotest.tools/v3 v3.0.2 // indirect
)

replace github.com/urfave/cli/v2 => github.com/mendersoftware/cli/v2 v2.1.1-minimal
