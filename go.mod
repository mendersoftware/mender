module github.com/mendersoftware/mender

go 1.13

require (
	github.com/bmatsuo/lmdb-go v0.0.0-20160816100615-69ad631904c9
	github.com/davecgh/go-spew v1.1.0
	github.com/konsorten/go-windows-terminal-sequences v1.0.2
	github.com/mendersoftware/log v0.0.0-20180403084710-7fef0b7a1659
	github.com/mendersoftware/mender-artifact v0.0.0-20191118115643-7c541c8f8348
	github.com/mendersoftware/mendertesting v0.0.0-20190301095837-dedd2c0a90a4
	github.com/mendersoftware/scopestack v0.0.0-20180403075023-2ce74757611b
	github.com/pkg/errors v0.0.0-20160916110212-a887431f7f6e
	github.com/pmezard/go-difflib v1.0.0
	github.com/remyoudompheng/go-liblzma v0.0.0-20190506200333-81bf2d431b96
	github.com/sirupsen/logrus v0.0.0-20180329225952-778f2e774c72
	github.com/stretchr/objx v0.1.0
	github.com/stretchr/testify v0.0.0-20190109162356-363ebb24d041
	github.com/ungerik/go-sysfs v0.0.0-20190613143942-7f098ddb67a6
	github.com/urfave/cli/v2 v2.2.0
	golang.org/x/crypto v0.0.0-20190308221718-c2843e01d9a2
	golang.org/x/net v0.0.0-20190724013045-ca1201d0de80
	golang.org/x/sys v0.0.0-20190924154521-2837fb4f24fe
	golang.org/x/text v0.3.2
)

replace github.com/urfave/cli/v2 => github.com/mendersoftware/cli/v2 v2.1.1-minimal
