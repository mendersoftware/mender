[![Build Status](https://travis-ci.org/mendersoftware/mender.svg?branch=master)](https://travis-ci.org/mendersoftware/mender)
[![Coverage Status](https://coveralls.io/repos/github/mendersoftware/mender/badge.svg?branch=master)](https://coveralls.io/github/mendersoftware/mender?branch=master)

Mender: Securing the world's connected devices
==============================================

Mender is an open source over-the-air (OTA) software updater for connected Linux
devices.

Safely and securely update your fleet of devices with Mender's client/server
solution while managing all the security nuances required for the update
process.

Built by the team behind the battle-hardened configuration management framework
that currently manages over 10 million Linux-based systems, Mender aims to
provide an OTA software updates product to fix software bugs, mend security
vulnerabilities, and deliver new features to Linux devices.

![Mender logo](mender_logo.png)

## Our mission

We want to help organizations make their internet connected products more secure
by offering a series of inexpensive, open and ease to use software solutions. We
shall be easy to deal with and approach everyone we deal with in an open and
transparent manner. Customers don’t choose us because we deliver the most
bleeding-edge solution, but because the combination of open and easy to use
greatly reduces the risks and aversion against making changes to their products.


## Our promise

“Our promise is to offer superior customer support and ***easy to use***
software applications that individually and collectively will make your
connected products more secure against data breaches”


## Target audience

Mender is a general purpose solution aimed at embedded system developers across
any industry.


## Current status

Mender is in rapid development. Currently, we have a working Mender client and
Yocto meta-mender layer. This allows you to build a [Yocto
image](https://www.yoctoproject.org?target=_blank) with Mender for Beaglebone
and QEMU, and conduct a full local image update. If booting of the new image
fails, Mender will automatically roll-back to the previous working image. Soon,
we will release a server backend that allows you to conduct a basic end-to-end
image based update.


## Getting started

To start using Mender, we recommend that you begin with the [Getting started
section in the documentation](https://docs.mender.io/).


Contributing to Mender
======================

We welcome and ask for your contribution. If you would like to contribute to Mender, please read our guide on how to best get started [contributing code or
documentation](https://github.com/mendersoftware/mender/blob/master/CONTRIBUTING.md).

## Licensing

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
* Fork us on [Github](https:github.com/mendersoftware)
* Email us at [contact@mender.io](mailto:contact@mender.io)
