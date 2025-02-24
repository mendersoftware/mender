test
![Mender logo](mender_logo.png)

[![Build Status](https://gitlab.com/Northern.tech/Mender/mender/badges/master/pipeline.svg)](https://gitlab.com/Northern.tech/Mender/mender/pipelines)
[![Coverage Status](https://coveralls.io/repos/github/mendersoftware/mender/badge.svg?branch=master)](https://coveralls.io/github/mendersoftware/mender?branch=master)

# Overview

Mender is an open-source, over-the-air (OTA) update manager for IoT and embedded Linux devices. Its
client-server architecture enables the central management of software deployments, including
functionality such as dynamic grouping, phased deployments, and delta updates. Mender also supports
powerful extensions to [configure](https://mender.io/product/features/device-configuration),
[monitor](https://mender.io/product/features/device-monitoring), and
[troubleshoot](https://mender.io/product/features/device-troubleshooting) devices. Features include
remote terminal access, port forwarding, file transfer, and device configuration. It integrates with
[Azure IoT Hub](https://azure.microsoft.com/en-us/products/iot-hub/) and [AWS IoT
core](https://aws.amazon.com/iot-core/).

## Table of Contents

* [Why Mender?](#why-mender)
* [Where to start?](#where-to-start)
* [Mender documentation](#mender-documentation)
* [About this repository](#about-this-repository)
* [Contributing](#contributing)
* [License](#license)
* [Security disclosure](#security-disclosure)
* [Installing from source](#installing-from-source)
* [Cross-compiling](#cross-compiling)
* [Community](#community)
* [Authors](#authors)

## Why Mender?

Mender enables secure and robust over-the-air updates for all device software. Some of the core
functionalities include:

* 💻 Flexible management server and client architecture for secure OTA software update deployments
  and fleet management.
* 💾 Standalone deployment support, triggered at the device-level (**no server needed**) for
  unconnected or USB delivered software updates.
* 🔄 Automatic rollback upon update failure with an A/B partition design.
* 🔀 Support for a full root file system, application, files, and containerized updates.
* ✅ Dynamic grouping, phased rollouts to ensure update success.
* ⚙️ Advanced configuration, monitoring, and troubleshooting for software updates.
* 🔬 Extensive logging, audits, reporting, and security and regulatory compliance capabilities.

### Our mission and goals

Embedded product teams often create homegrown updaters at the last minute due to the need to fix
bugs in field-deployed devices. However, the essential requirement for an embedded update process is
*robustness*. For example, loss of power at any time should not brick a device. This creates a
challenge, given the time constraints to develop and maintain a homegrown updater.

**Mender aims to address this challenge with a *robust* and *easy to use* updater for embedded Linux
devices, which is open source and available to anyone.**

Robustness is ensured with *atomic* image-based deployments using a dual A/B rootfs partition
layout. This makes it always possible to roll back to a working state, even when losing power at any
time during the update process.

Ease of use is addressed with an intuitive UI, [comprehensive
documentation](https://docs.mender.io/), a [meta layer for the Yocto
Project](https://github.com/mendersoftware/meta-mender) for *easy integration into existing
environments*, and high-quality software (see the test coverage badge).

## Where to start?

| **Mender enterprise**| **Mender Open Source** |
| ------------- | ------------- |
| Ready to get started on an enterprise-grade OTA software update solution? Capabilities include advanced fleet management, security, and compliance: role-based access control (RBAC), dynamic groups, delta updates, and mutual TLS support. Get started with [hosted Mender](https://hosted.mender.io/ui/signup) and evaluate Mender for free. | Alternatively, the Mender open-source option allows you to start doing smart device updates in a quick, secure, and robust method. Check out [how to get started](https://docs.mender.io/get-started). <br /> In order to support rollback, the Mender client depends on integration with the boot loader and the partition layout. It is, therefore, most easily built as part of your Yocto Project image by using the [meta layer for the Yocto Project](https://github.com/mendersoftware/meta-mender). |

If you want to compare the options available, look at our
[features](https://mender.io/product/features) page.

### Mender documentation

The [documentation](https://docs.mender.io) is a great place to learn more, especially:

* [Overview](https://docs.mender.io/overview/introduction) — learn more about Mender, it's design,
  and capabilities.
* [Debian](https://docs.mender.io/system-updates-debian-family) — get started with updating your
  Debian devices.
* [Yocto](https://docs.mender.io/system-updates-yocto-project) — take a look at our support for
  Yocto.

Would you rather dive into the code? Then you are already in the right place!

---

# High-level architecture overview

![Mender architecture](mender_architecture.png)
<!-- https://drive.google.com/file/d/1pKfJ-eRHYrDYZW7jCTHI7_D9tEF-rKDz -->

The chart above depicts a typical Mender architecture with the following elements:

* Back End & User Interface: The shaded sky blue area is the Mender product which comprises the back end and the user interface (UI).
* Clients: The Mender client runs on the devices represented by the devices icon.
* Gateway: All communications between devices, users, and the back end occur through an API gateway. [Traefik](https://traefik.io) is used for the API gateway. The gateway routes the requests coming from the clients to the right micro-service(s) in the Mender back end.
* NATS message broker: some of the micro-services use NATS as a message broker to support the Mender device update troubleshooting and the orchestration within the system.
* Mongo DB: persistent database for the Mender back end micro-services.
* Storage layer: in both hosted and on-premise Mender, an AWS S3 Bucket (or S3 API-compatible) or an Azure Storage Account storage layer is used to store the artifacts.
* Redis: in-memory cache to enable device management at scale.

You can find more detailed information in our [documentation](https://docs.mender.io/3.5/server-installation/overview).

---

# About this repository

This repository contains the Mender client updater, which can be run in standalone mode (manually
triggered through its command line interface) or managed mode (connected to the Mender server).

Mender provides both the client-side updater and the backend and UI for managing deployments as open
source. The Mender server is designed as a microservices architecture and comprises several
repositories.

## Contributing

We welcome and ask for your contribution. If you would like to contribute to Mender, please read our guide on how to best get started [contributing code or
documentation](https://github.com/mendersoftware/mender/blob/master/CONTRIBUTING.md).

## License

Mender is licensed under the Apache License, Version 2.0. See
[LICENSE](https://github.com/mendersoftware/mender/blob/master/LICENSE) for the full license text.

## Security disclosure

We take security very seriously. If you come across any issue regarding
security, please disclose the information by sending an email to
[security@mender.io](security@mender.io). Please do not create a new public
issue. We thank you in advance for your cooperation.

## Installing from source

### Requirements

* C++ compiler
* cmake
* libarchive-dev, libboost-all-dev, liblmdb-dev, libdbus-1-dev, libssl-dev and libsystemd-dev packages

For Debian/Ubuntu, the prerequisites can be installed by
```
sudo apt install git build-essential cmake libarchive-dev liblmdb-dev libboost-all-dev libssl-dev libdbus-1-dev libsystemd-dev
```
Adjust as needed for other distributions.


### Steps

To install Mender C++ client on a device from source, first clone the repository:

```
git clone https://github.com/mendersoftware/mender.git
```

Change into the cloned repository:
```
cd mender
```

Use `git submodule` to fetch additional dependencies:
```
git submodule update --init --recursive
```

Create a build directory and enter it:
```
mkdir build
cd build
```

Configure and start the build:

```
cmake -DCMAKE_INSTALL_PREFIX:PATH=/usr ..
make
```
Make sure you configure installation to the `/usr` path prefix, as the executables required by systemd units and
D-Bus policy file must be placed in the canonical paths.

Install the client:
```
sudo make install
```

### Client setup

The required configuration files for client operation can be created in several ways.

- for an interactive setup process, use the [`mender-setup`](https://github.com/mendersoftware/mender-setup) tool.
- in a Yocto-based set up, those are generated as part of the build process

### Installation notes

Installing this way does not offer a complete system updater.  For this, you need additional
integration steps. Depending on which OS you are using, consult one of the following:

* [System updates: Debian family](https://docs.mender.io/system-updates-debian-family)
* [System updates: Yocto Project](https://docs.mender.io/system-updates-yocto-project)

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

**Important:** `demo.crt` is not a secure certificate and should only be used for demo purposes,
never in production.

## Cross-compiling

Generic cross-compilation procedures using `cmake` apply.

During the current, early stage of development using a higher, cross-compilation aware build
system such as Yocto is advisable. Once things are sufficiently stabilized, a set of steps for
manual cross-compilation will be added here.

### QNX

**Note that QNX support is still experimental, and not supported.**

```
mkdir build && cd build
QNX_TARGET_ARCH=aarch64le cmake .. -DCMAKE_TOOLCHAIN_FILE=../cmake/qnx.cmake
make
```


## Running

Once installed, Mender can be enabled by executing:

```
systemctl enable mender-authd mender-updated && systemctl start mender-authd mender-updated
```

## D-Bus API

The introspection files for Mender D-Bus API can be found at
[documentation](https://docs.mender.io/device-side-api)

## Community

* Join the [Mender Hub discussion forum](https://hub.mender.io)
* Follow us on [Twitter](https://twitter.com/mender_io). Please
  feel free to tweet us questions.
* Fork us on [GitHub](https://github.com/mendersoftware)
* Create an issue in the [bugtracker](https://tracker.mender.io/projects/MEN)
* Email us at [contact@mender.io](mailto:contact@mender.io)
* Connect to the [#mender IRC channel on Libera](https://web.libera.chat/?#mender)

## Authors

Mender was created by the team at [Northern.tech AS](https://northern.tech), with many contributions
from the community. Thanks [everyone](https://github.com/mendersoftware/mender/graphs/contributors)!

### About Northern.tech

[Northern.tech](https://northern.tech) is the leader in device lifecycle management with a mission to secure the world's
connected devices. Established in 2008, Northern.tech showcases a long history of enterprise
technology management before IIoT and IoT became buzzwords. Northern.tech is the company behind
[CFEngine](https://cfengine.com), a standard in server configuration management, to automate
large-scale IT operations and compliance.

Learn more about [device lifecycle management](https://northern.tech/what-we-do) for industrial IoT devices.
