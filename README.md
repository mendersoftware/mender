[![Build Status](https://travis-ci.org/mendersoftware/mender.svg?branch=master)](https://travis-ci.org/mendersoftware/mender)
[![codecov](https://codecov.io/gh/mendersoftware/mender/branch/master/graph/badge.svg)](https://codecov.io/gh/mendersoftware/mender)

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

### Steps

To install Mender on a device from source, please run the following commands
inside the cloned repository:

```
make
sudo make install
```

Installing this way does not offer a complete system updater. For this you need
[additional integration steps](https://docs.mender.io/devices). However, it is
possible to use [update
modules](https://docs.mender.io/development/artifacts/update-modules) and update
other parts of the system.

In order to connect to a Mender server, you either need to get a [Hosted
Mender](https://hosted.mender.io/) account, or [set up a server
environment](https://docs.mender.io/getting-started/create-a-test-environment). If
you are setting up your a demo environment, you will need to put the
`support/demo.crt` file into `/etc/mender/server.crt` on the device and add this
to `/etc/mender/mender.conf` after the installation steps above:

```
  "ServerCertificate": "/etc/mender/server.crt"
```

Keep in mind that `/etc/mender/mender.conf` will be overwritten if you rerun the
`sudo make install` command.

**Important:** `demo.crt` is not a secure certificate, and should only be used
for demo purposes, never in production.

### Running

Once installed, Mender can be enabled by executing:

```
systemctl enable mender && systemctl start mender
```

## Connect with us

* Join our [Google
  group](https://groups.google.com/a/lists.mender.io/forum/#!forum/mender)
* Follow us on [Twitter](https://twitter.com/mender_io?target=_blank). Please
  feel free to tweet us questions.
* Fork us on [Github](https://github.com/mendersoftware)
* Email us at [contact@mender.io](mailto:contact@mender.io)
