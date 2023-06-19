module github.com/mendersoftware/mender

go 1.14

require (
	github.com/bmatsuo/lmdb-go v1.6.1-0.20160816100615-69ad631904c9
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/godbus/dbus v4.1.0+incompatible
	github.com/gorilla/websocket v1.4.3-0.20220104015952-9111bb834a68
	github.com/mendersoftware/mender-artifact v0.0.0-20230125055725-c322771c6a2c
	github.com/mendersoftware/openssl v1.1.1-0.20221101131127-8797d18baf1a
	github.com/mendersoftware/progressbar v0.0.3
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.0
	github.com/stretchr/testify v1.8.1
	github.com/ungerik/go-sysfs v0.0.0-20190613143942-7f098ddb67a6
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/sys v0.9.0
	golang.org/x/term v0.9.0
)

replace github.com/urfave/cli/v2 => github.com/mendersoftware/cli/v2 v2.1.1-minimal
