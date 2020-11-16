module github.com/mendersoftware/mender

go 1.14

require (
	github.com/bmatsuo/lmdb-go v1.6.1-0.20160816100615-69ad631904c9
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/godbus/dbus v4.1.0+incompatible
	github.com/mendersoftware/mender-artifact v0.0.0-20201001154130-4a2b5b98775d
	github.com/mendersoftware/mendertesting v0.0.1
	github.com/mendersoftware/openssl v1.0.10
	github.com/pkg/errors v0.9.1
	github.com/remyoudompheng/go-liblzma v0.0.0-20190506200333-81bf2d431b96 // indirect
	github.com/sirupsen/logrus v1.4.3-0.20200306102446-7ea96a3284ed
	github.com/stretchr/testify v1.6.1
	github.com/ungerik/go-sysfs v0.0.0-20190613143942-7f098ddb67a6
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/crypto v0.0.0-20191117063200-497ca9f6d64f
	golang.org/x/net v0.0.0-20191119073136-fc4aabc6c914
	golang.org/x/sys v0.0.0-20191120155948-bd437916bb0e
	golang.org/x/text v0.3.2 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200615113413-eeeca48fe776 // indirect
)

replace github.com/urfave/cli/v2 => github.com/mendersoftware/cli/v2 v2.1.1-minimal
