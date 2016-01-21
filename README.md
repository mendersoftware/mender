[![Build Status](https://travis-ci.org/mendersoftware/mender.svg?branch=master)](https://travis-ci.org/mendersoftware/mender)
[![Coverage Status](https://coveralls.io/repos/github/mendersoftware/mender/badge.svg?branch=master)](https://coveralls.io/github/mendersoftware/mender?branch=master)

# Mender 

Mender is a framework for automate over-the-air software updates for embedded devices.
In order to test it, it is strongly recommended to build it as a part of a Yocto image
(Yocto layers provided in mendersoftware/meta-mender and mendersoftware/meta-mender-qemu)
as in addition to Mender binary you will need to have certain configuration of your bootloader and
one of the supported partitioning schemes.


1. How it works?
================

The idea of Mender is to be able to update whole image of your embedded device. At the moment it is possible
to update image containing kernel and rootfs. Updating bootloader is considered less important and more
risky thus first version of the application is not designed to be able to update bootloader.


2. Partitioning target device
=============================

The idea is to have at least 3 different partitions one of which is boot partition and remaining 2 partitions
where kernel and rootfs together are stored. One of the partitions will be used
as active partition, from which kernel and rootfs will be booted, second one will be used by the update mechanism to store new image. Second partition will be referenced as
inactive later in this document.

It is also possible to use yet another partition where all user data will be stored
so that after update all important data will be preserved.

Example of partitioning schema is shown below:

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

At the moment Mender is designed to be used with u-boot bootloader. Decision was made based on extreme flexibility and
popularity of it in embedded devices. What is more, it is open source so any modifications and adjustments
for different needs are possible.

Besides special configuration for your board, u-boot needs to be compiled and used with feature referenced
as 'Boot Count Limit' (http://www.denx.de/wiki/view/DULG/UBootBootCountLimit). It allows to use u-boot to perform
special actions when booting process fails certain amount of times.

Also support for modifying u-boot variables from userspace is required so that fw_printenv/fw_setenv utilities
are available in userspace. Those utilities can be compiled from u-boot sources and are part of u-boot. More can be found here:
http://www.denx.de/wiki/view/DULG/HowCanIAccessUBootEnvironmentVariablesInLinux


4. Boot process
===============

TBD



5. Testing Mender with Yocto and Qemu
=====================================

For the simplicity and test purposes Mender can be tested using Qemu emulator. Detailed instructions how to build
Yocto image that can be run and tested in qemu are provided in meta-mender-qemu repository.


6. Running Mender
=================

Please note that the process described here is mostly manual as the work on client and server side application is still in progress.
It will be fully automated in future with a possibility of automatic rollback when update process fails.

What is more, as Mender is not only stand-alone application but rather a framework and needs
integration with bootloader as well as requires certain partitioning schema it is recommended to use it together with
provided Yocto Mender layers for self-containing image build. It is possible though to use it independently after providing all needed dependencies and
requirements.

Assuming that all the dependencies are resolved, in order to use Mender you need to run:

    $ mender -rootfs image

where image is complete image containing kernel and rootfs. This command will install image on inactive
partition and set needed u-boot variables so that after restart system will be booted from inactive partition
using freshly updated kernel and rootfs.

Please note that at the moment image must be manually delivered to the device, but in future this will be
done automatically and will be event-driven process where Mender client will communicate with server in 
order to get notifications when new update is scheduled and to fetch the image that will be used to update
device.


6A. Successful update
=====================

After fetching new image, installing using 'mender -rootfs image', reboot and (currently manual) verification that the new image is working correctly you need to commit
changes and inform Mender that the new image is running correctly. In order to do so run:

    $ mender -commit

This will set all needed boot configuration so that after device will be restarted again it will be booted from
updated partition and thus it (inactive partition) will become new active partition. The process can then be repeated so that
new image will be installed to new inactive partition which then can be tested and verified.

6B. Failing update
==================

If for some reason new image is broken, or userspace verification is failing device will be rolled-back to previously
working image. This is possible as having 2 kernel and rootfs partitions you can always have one version of kernel and
rootfs which is verified to work correctly and one which is used to store updated version.

In order to simulate this behavior after performing update (with 'mender -rootfs image') and rebooting device simply
don't run 'mender -commit' command. This will cause that boot variables won't be updated and after next reboot
device will be booted from active partition (not updated one) instead.

6C. Automatic roll-back
=======================

Also if for some reason u-boot will detect that newly updated image can not be booted after defined number of tries
(this can be configured by setting 'bootlimit' u-boot variable) device will be switched to roll-back mode
and partition with the update won't be used to boot. Instead, active partition will be used as this one was proven
to work correctly.


7. Project roadmap
==================

As already mentioned update process consist mostly of manual steps at the moment. There is work ongoing to make it
fully automatic so that image will be delivered to device automatically and whole update and roll-back process
will be automatic.
There is also work ongoing on the server side of the update framework, where you will be able to schedule image
updates and get the reports regarding the update status for each and every device bootstrapped to the server.







