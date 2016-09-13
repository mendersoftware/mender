[![Build Status](https://travis-ci.org/mendersoftware/mender.svg?branch=master)](https://travis-ci.org/mendersoftware/mender)
[![Coverage Status](https://coveralls.io/repos/github/mendersoftware/mender/badge.svg?branch=master)](https://coveralls.io/github/mendersoftware/mender?branch=master)

Mender: over-the-air updater for embedded Linux devices
==============================================

Mender is an open source over-the-air (OTA) software updater for embedded Linux
devices.

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

This repository contains the Mender client updater, which can be run in manual
mode or as a daemon (connected to the Mender server).

Mender not only provides the client-side updater, but also the backend and UI
for managing deployments as open source. The Mender server is
designed as a microservices architecture and comprises several repositories.


![Mender logo](mender_logo.png)


## Getting started

To start using Mender, we recommend that you begin with the Getting started
section in [the Mender documentation](https://docs.mender.io/).


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

## Connect with us

* Join our [Google
  group](https://groups.google.com/a/lists.mender.io/forum/#!forum/mender)
* Follow us on [Twitter](https://twitter.com/mender_io?target=_blank). Please
  feel free to tweet us questions.
* Fork us on [Github](https://github.com/mendersoftware)
* Email us at [contact@mender.io](mailto:contact@mender.io)
