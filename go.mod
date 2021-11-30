module github.com/mendersoftware/mender

go 1.14

require (
	github.com/bmatsuo/lmdb-go v1.6.1-0.20160816100615-69ad631904c9
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/godbus/dbus v4.1.0+incompatible
	github.com/mendersoftware/mender-artifact v0.0.0-20210927105858-af2a3e3fb006
	github.com/mendersoftware/openssl v1.1.0
	github.com/mendersoftware/progressbar v0.0.3
	github.com/pkg/errors v0.9.1
	github.com/remyoudompheng/go-liblzma v0.0.0-20190506200333-81bf2d431b96 // indirect
	github.com/sirupsen/logrus v1.4.3-0.20200306102446-7ea96a3284ed
	github.com/stretchr/testify v1.7.0
	github.com/ungerik/go-sysfs v0.0.0-20190613143942-7f098ddb67a6
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/crypto v0.0.0-20211115234514-b4de73f9ece8
	golang.org/x/sys v0.0.0-20211113001501-0c823b97ae02
	golang.org/x/term v0.0.0-20210927222741-03fcf44c2211 // indirect
)

replace github.com/urfave/cli/v2 => github.com/mendersoftware/cli/v2 v2.1.1-minimal
