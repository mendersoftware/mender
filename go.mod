module github.com/mendersoftware/mender

go 1.14

require (
	github.com/bmatsuo/lmdb-go v1.6.1-0.20160816100615-69ad631904c9
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/godbus/dbus v4.1.0+incompatible
	github.com/gorilla/websocket v1.4.3-0.20220104015952-9111bb834a68
	github.com/mendersoftware/mender-artifact v0.0.0-20220913084855-9ed8ad0d53d0
	github.com/mendersoftware/openssl v0.0.0-20220610125625-9fe59ddd6ba4
	github.com/mendersoftware/progressbar v0.0.3
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.8.1
	github.com/ungerik/go-sysfs v0.0.0-20190613143942-7f098ddb67a6
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/sys v0.0.0-20220128215802-99c3d69c2c27
	golang.org/x/term v0.0.0-20210927222741-03fcf44c2211
)

replace github.com/urfave/cli/v2 => github.com/mendersoftware/cli/v2 v2.1.1-minimal
