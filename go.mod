module github.com/mendersoftware/mender

go 1.14

require (
	github.com/bmatsuo/lmdb-go v1.6.1-0.20160816100615-69ad631904c9
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/mendersoftware/mender-artifact v0.0.0-20200327144921-a6d237202052
	github.com/mendersoftware/mendertesting v0.0.1
	github.com/mendersoftware/openssl v1.0.10
	github.com/pkg/errors v0.7.2-0.20160916110212-a887431f7f6e
	github.com/remyoudompheng/go-liblzma v0.0.0-20190506200333-81bf2d431b96 // indirect
	github.com/sirupsen/logrus v1.4.3-0.20200306102446-7ea96a3284ed
	github.com/stretchr/testify v1.6.1
	github.com/ungerik/go-sysfs v0.0.0-20190613143942-7f098ddb67a6
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/crypto v0.0.0-20190923035154-9ee001bba392
	golang.org/x/net v0.0.0-20190724013045-ca1201d0de80
	golang.org/x/sys v0.0.0-20190924154521-2837fb4f24fe
	golang.org/x/text v0.3.2 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200615113413-eeeca48fe776 // indirect
)

replace github.com/urfave/cli/v2 => github.com/mendersoftware/cli/v2 v2.1.1-minimal
