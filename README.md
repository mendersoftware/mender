[![Build Status](https://gitlab.com/Northern.tech/Mender/mender/badges/master/pipeline.svg)](https://gitlab.com/Northern.tech/Mender/mender/pipelines)
[![Coverage Status](https://coveralls.io/repos/github/mendersoftware/mender/badge.svg?branch=master)](https://coveralls.io/github/mendersoftware/mender?branch=master)

Mender: over-the-air updater for embedded Linux devices
==============================================

Mender is an open source over-the-air (OTA) software updater for embedded Linux
devices. Mender comprises a client running at the embedded device, as well as
a server that manages deployments across many devices.

Embedded product teams often end up creating homegrown updaters at the last
minute due to the need to fix bugs in field-deployed devices. However, the most
important requirement for an embedded update process is *robustness*, for example
loss of power at any time should not brick a device. This creates a challenge
given the time constraints to develop and maintain a homegrown updater.

Mender aims to address this challenge with a *robust* and *easy to use* updater
for embedded Linux devices, which is open source and available to anyone.

Robustness is ensured with *atomic* image-based deployments using a dual A/B
rootfs partition layout. This makes it always possible to roll back to a working state, even
when losing power at any time during the update process.

Ease of use is addressed with an intuitive UI, [comprehensive documentation](https://docs.mender.io/), a
[meta layer for the Yocto Project](https://github.com/mendersoftware/meta-mender) for *easy integration into existing environments*,
and high quality software (see the test coverage badge).

This repository contains the Mender client updater, which can be run in standalone mode
(manually triggered through its command line interface) or managed mode (connected to the Mender server).

Mender not only provides the client-side updater, but also the backend and UI
for managing deployments as open source. The Mender server is
designed as a microservices architecture and comprises several repositories.


![Mender logo](mender_logo.png)


## Getting started

To start using Mender, we recommend that you begin with the Getting started
section in [the Mender documentation](https://docs.mender.io/).

In order to support rollback, the Mender client depends on integration with
U-Boot and the partition layout. It is therefore most easily built as part of
your Yocto Project image by using the
[meta layer for the Yocto Project](https://github.com/mendersoftware/meta-mender).


## Contributing

We welcome and ask for your contribution. If you would like to contribute to Mender, please read our guide on how to best get started [contributing code or
documentation](https://github.com/mendersoftware/mender/blob/master/CONTRIBUTING.md).

## License

Mender is licensed under the Apache License, Version 2.0. See
[LICENSE](https://github.com/mendersoftware/mender/blob/master/LICENSE) for the
full license text.

## Security disclosure

We take security very seriously. If you come across any issue regarding
security, please disclose the information by sending an email to
[security@mender.io](security@mender.io). Please do not create a new public
issue. We thank you in advance for your cooperation.

## Installing from source

### Requirements

* C compiler
* [Go compiler](https://golang.org/dl/)
* liblzma-dev, libssl-dev and libglib2.0-dev packages

#### LZMA support opt-out

If no LZMA Artifact compression support if desired, you can ignore the `liblzma-dev` package
dependency and substitute the `make` commands in the instructions below for:

```
make TAGS=nolzma
```

#### D-Bus support opt-out

If no D-Bus support if desired, you can ignore the `libglib2.0-dev` package dependency and substitute
the `make` commands in the instructions below for:

```
make TAGS=nodbus
```

### Steps

To install Mender on a device from source, first clone the repository in the correct folder
structure inside your `$GOPATH` (typically `$HOME/go`):

```
git clone https://github.com/mendersoftware/mender.git $GOPATH/src/github.com/mendersoftware/mender
```

Then run the following commands inside the cloned repository:

```
make
sudo make install
```

### Installation notes

Installing this way does not offer a complete system updater.
For this you need additional integration steps, depending in which OS you
are using consult one of the following:

- [System updates: Debian family](https://docs.mender.io/system-updates-debian-family)
- [System updates: Yocto Project](https://docs.mender.io/system-updates-yocto-project)

However, it is possible to use [Update Modules](https://docs.mender.io/artifacts/update-modules)
and update other parts of the system.

In order to connect to a Mender server, you either need to get a [Mender
Professional](https://hosted.mender.io/) account, or [set up a server
environment](https://docs.mender.io/getting-started/create-a-test-environment). If
you are setting up a demo environment, you will need to put the
`support/demo.crt` file into `/etc/mender/server.crt` on the device and add the
configuration line below to `/etc/mender/mender.conf` after the installation
steps above:

```
  "ServerCertificate": "/etc/mender/server.crt"
```

Keep in mind that `/etc/mender/mender.conf` will be overwritten if you rerun the
`sudo make install` command.

**Important:** `demo.crt` is not a secure certificate, and should only be used
for demo purposes, never in production.

## Cross-compiling

### Requirements

* C cross-compiler for the target platform
* [Go compiler](https://golang.org/dl/)

### Build steps

#### Cross-compiler setup

Download the cross-compiler required for your device. Then add the cross-compiler `bin/`
subfolder in your path and set the `CC` variable accordingly using the commands:

```
export PATH=$PATH:<path_to_my_cross_compiler>/bin
export CC=<cross_compiler_prefix>
```

For instance, to cross-compiling for Raspberry Pi:

```
git clone https://github.com/raspberrypi/tools.git
export PATH="$PATH:$(pwd)/tools/arm-bcm2708/gcc-linaro-arm-linux-gnueabihf-raspbian-x64/bin"
export CC=arm-linux-gnueabihf-gcc
```

#### liblzma dependency

Download, extract, compile, and install liblzma with the following commands:

```
wget -q https://tukaani.org/xz/xz-5.2.4.tar.gz
tar -xzf xz-5.2.4.tar.gz
cd xz-5.2.4
./configure --host=<target-arch> --prefix=$(pwd)/install
make
make install
```

Where `target-arch` should match your device toolchain, for example `arm-linux-gnueabihf`

Export an environment variable for later use:

```
export LIBLZMA_INSTALL_PATH=$(pwd)/install
```

### Build steps

Now, to cross-compile Mender, run the following commands inside the cloned repository:

```
make CGO_CFLAGS="-I${LIBLZMA_INSTALL_PATH}/include" CGO_LDFLAGS="-L${LIBLZMA_INSTALL_PATH}/lib" \
CGO_ENABLED=1 GOOS=linux GOARCH=<arch>
```

Where `arch` is the target architecture (for example `arm`). See all possible values for `GOARCH` in the [source code](https://github.com/golang/go/blob/master/src/go/build/syslist.go). Also note that for `arm` architecture you also need to specify which family to compile for
with `GOARM`; for more information see [this link](https://github.com/golang/go/wiki/GoArm)

You can deploy the mender client file tree in a custom directory in order to send it
to your device afterwards. To deploy all mender client files in a custom directory,
run the command:

```
make prefix=<custom-dir> install
```

Where `custom-dir` is the destination folder for your file tree

Finally, copy this file tree into your target's device rootfs. You can do it remotely
using SSH, for example.

See also [Installation notes](#installation-notes)

### Running

Once installed, Mender can be enabled by executing:

```
systemctl enable mender-client && systemctl start mender-client
```

## Connect with us

* Join the [Mender Hub discussion forum](https://hub.mender.io)
* Follow us on [Twitter](https://twitter.com/mender_io). Please
  feel free to tweet us questions.
* Fork us on [GitHub](https://github.com/mendersoftware)
* Create an issue in the [bugtracker](https://tracker.mender.io/projects/MEN)
* Email us at [contact@mender.io](mailto:contact@mender.io)
* Connect to the [#mender IRC channel on Freenode](http://webchat.freenode.net/?channels=mender)


## Authors

Mender was created by the team at [Northern.tech AS](https://northern.tech), with many contributions from
the community. Thanks [everyone](https://github.com/mendersoftware/mender/graphs/contributors)!

[Mender](https://mender.io) is sponsored by [Northern.tech AS](https://northern.tech).
