module github.com/mendersoftware/mender

go 1.14

require (
	github.com/Linutronix/golang-openssl v0.0.0-20200515123529-9ba73c929a0a
	github.com/bmatsuo/lmdb-go v1.6.1-0.20160816100615-69ad631904c9
	github.com/mendersoftware/mender-artifact v0.0.0-20200327144921-a6d237202052
	github.com/mendersoftware/mendertesting v0.0.1
	github.com/pkg/errors v0.7.2-0.20160916110212-a887431f7f6e
	github.com/sirupsen/logrus v1.4.3-0.20200306102446-7ea96a3284ed
	github.com/stretchr/testify v1.6.1
	github.com/ungerik/go-sysfs v0.0.0-20190613143942-7f098ddb67a6
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/crypto v0.0.0-20190923035154-9ee001bba392
	golang.org/x/net v0.0.0-20190724013045-ca1201d0de80
	golang.org/x/sys v0.0.0-20190924154521-2837fb4f24fe
)

replace github.com/urfave/cli/v2 => github.com/mendersoftware/cli/v2 v2.1.1-minimal
