[![Build Status](https://travis-ci.org/mendersoftware/mender.svg?branch=master)](https://travis-ci.org/mendersoftware/mender)
[![Coverage Status](https://coveralls.io/repos/github/mendersoftware/mender/badge.svg?branch=master)](https://coveralls.io/github/mendersoftware/mender?branch=master)

# Mender

Mender is a framework for automating over-the-air software updates to
Linux-based embedded devices.

In order to test it, it is strongly recommended to build it as a part
of a Yocto image, as you will need to have the bootloader and
partition layout set up in a specific way.  Yocto layers are provided
in the
[meta-mender](https://www.github.com/mendersoftware/meta-mender)
repository.

1. How it works
===============

Mender performs a full image update of the embedded device. Currently,
both the kernel and rootfs are expected to be part of the image
update. Updating the bootloader is considered less important and more
risky, so the first version of Mender is not designed to update the
bootloader.


2. Partitioning target device
=============================

At least three different partitions are required, one of which is the
boot partition, and the remaining two partitions are where both the
kernel and rootfs are stored. One of the partitions will be used as
active partition, from which the kernel and rootfs will be booted, the
second one will be used by the update mechanism to write the updated
image. The second partition will be referred to as "inactive" later in
this document.

It is also possible to use yet another partition to store persistent
user data, so this does not get overwritten during an update.

A sample partition layout is shown below:

```
           +--------+
           | EEPROM |
           +--------+
     +----------------------+
     | boot partition (FAT) |
     +----------------------+
    +-----------+-----------+
    | rootfs    |  rootfs   |
    | kernel    |  kernel   |
    +-----------+-----------+
          +------------+
          | user data  |
          | (optional) |
          +------------+
```

3. Bootloader support
=====================

Mender is currently designed to be used with the U-Boot
bootloader. The choice of U-Boot was made based on its flexibility and
popularity in embedded devices. It is also open source, which enables
any required modifications or adjustments.

Besides any special configuration to support the device, U-Boot needs
to be compiled and used with a feature known as as
[Boot Count Limit](http://www.denx.de/wiki/view/DULG/UBootBootCountLimit). It
enables specific actions to be triggered when the boot process fails a
certain amount of attempts.

Support for modifying U-Boot variables from userspace is also required
so that fw_printenv/fw_setenv utilities are available in
userspace. These utilities
[can be compiled from U-Boot sources](http://www.denx.de/wiki/view/DULG/HowCanIAccessUBootEnvironmentVariablesInLinux)
and are part of U-Boot.



4. Boot process
===============

TBD



5. Testing Mender with Yocto and QEMU
=====================================

To quickly get started, Mender can be tested using the QEMU emulator.
Detailed instructions how to build a Yocto image that can be run and
tested in QEMU are provided in the
[meta-mender repository](https://www.github.com/mendersoftware/meta-mender).


6. Running Mender
=================

Please note that the process described here is mostly manual as the
development of both the client and server component is still in
progress.  It will be fully automated in the future, including the
ability to of automatically roll back when update process fails.

What is more, as Mender is not a stand-alone application, but needs
integration with the bootloader and requires certain partitioning
schema, it is recommended to use it with the provided Yocto Mender
layers for a self-containing image build. It is however possible to
use it independently by setting up the needed dependencies and
requirements.

Assuming that all the dependencies are resolved, in order to use
Mender you need to run:

    $ mender -rootfs image

where `image` is a complete image containing the kernel and
rootfs. This command will install the image on the inactive partition
and set the needed U-Boot variables so that after a restart, the
system will be booted from the inactive partition and use the freshly
updated kernel and rootfs.

Please note that `image` can be a http URL or be manually copied to
the local file device system, but in the future this will be done
automatically and will be an event-driven process. The Mender client
will communicate with the server in order to get notifications when a
new update is scheduled and then fetch the image that will be used to
update device.


6A. Successful update
=====================

After fetching a new image, installing using `mender -rootfs image`,
reboot and (currently manual) verification that the new image is
working correctly you need to commit changes and inform Mender that
the new image is running correctly. In order to do so run:

    $ mender -commit

This will set all the required boot configurations so that after
device will be restarted again it will be booted from the updated
partition and thus the inactive partition will become new active
partition.  The process can then be repeated so that a new image will
be installed to the new inactive partition which then can be tested
and verified.

6B. Failing update
==================

If for some reason the new image is broken, or userspace verification
is failing, the device will be rolled back to the previously working
image. This is possible by having two kernel and rootfs partitions, as
you can always have one version of the kernel and rootfs which is
verified to work correctly and one which is used to write the update.

In order to simulate this behavior after performing the update with
`mender -rootfs image` and rebooting device, simply don't run the
`mender -commit` command. This will cause boot variables to not be
updated and after the next reboot, the device will be booted from the
active partition (not updated one) instead.

6C. Automatic roll-back
=======================

Also if for some reason U-Boot will detect that the newly updated
image can not be booted after a defined number of tries (this can be
configured by setting `bootlimit` U-Boot variable), the device will be
switched to roll-back mode and the partition with the update won't be
used to boot. Instead, the active partition will be used as it has
been proven to work correctly.

7. Platform integration
=======================

Mender requires certain functionality to be provided by platform
integration glue. This covers performing certain operations on the
device or obtaining information about platform that Mender is running
on.

Platform integration is implemented with use of helper tools that
Mender will call during runtime. The tools are named using `mender-`
prefix (for example `mender-device-identity`) and need to be placed
in `/usr/bin/`.

Each tool needs to conform to the common protocol. Since tools are
invoked during runtime, a successful execution is indicated by 0 exit
status. A failure to execute or perform an operation is indicated by a
non-0 status.

7A. Device identity
===================

Each device is identified in the system based on its identity data, a
set of attributes unique to each device. This set can be different for
every platform, but devices sharing a common platform should use a
similar set of attributes.

Since device identity is platform specific Mender relies on
`mender-device-identity` helper tool to collect and produce the set of
unique attributes.

Mender will run `mender-device-identity` tool reading its standard
output. The tool shall output device attributes in `key=value` format,
each attribute on a separate line ending with `\n`, for example:

```
mac=00:11:22:33:44:55
cpuid=12331-ABC
SN=123551
```

An example script is provided in `support/` directory within the
tree.

8. Project roadmap
==================

As mentioned, the update process mostly consists of manual steps at
the moment. There is ongoing work to make it fully automated so that
the image will be delivered to device automatically and the whole
update and roll-back process will be automatic.  There is also ongoing
work on the server side of the update framework, where you will be
able to schedule image updates and get the reports for the update
status for each and every device connected to the server.
