Mender Client Setup Guide
==========================

The Mender Client needs to be set up so that it can respect the parameters of
the device and connect to the desired Mender Server. Depending on the
installation mechanism, the setup process can be part of the installation
procedure (utilizing the `mender-setup` tool), using interactive prompts, or it
needs to be performed manually after installation. This guide focuses on the
latter case.


Persistent Storage
-------------------

The Mender Client requires space to store persistent data, i.e. data that is not
overwritten when Mender Artifacts are installed on the system. A typical
solution for such persistent storage is a separate partition mounted at `/data`,
but to allow for greater flexibility, the Mender Client uses `/var/lib/mender`
path as the root directory under with it stores all its persistent data, which
is typically a symbolic link to `/data/mender`. Check if `/var/lib/mender`
exists and is a directory or a symlink by running:

```
ls -l /var/lib/mender
```

If the output shows a symbolic link, for example:

```
lrwxrwxrwx 1 root root 12 Dec  2 11:41 /var/lib/mender -> /data/mender
```

then make sure the target is actually on some storage not overwritten by Mender
Artifacts.

If the path doesn't exist, please create a symlink to an appropriate persistent
storage by running (replacing `the/persistent/storage` accordingly):

```
ln -s /the/persistent/storage/mender /var/lib/mender
```

If the path exists, but is not a symlink and is not on appropriate persistent
storage, move it aside by running:

```
mv /var/lib/mender{,.orig}
```

then create the symlink (as suggested above):

```
ln -s /the/persistent/storage/mender /var/lib/mender
```

and then move **contents** of the `/var/lib/mender.orig` directory into the new
location:

```
mv /var/lib/mender.orig/* /var/lib/mender.orig/.* /var/lib/mender/
```


Device Type
------------

One of the key attributes of a device the Mender Client needs to know is the
_Device Type_. This attribute is reported to the Mender Server and both the
Server and the Client use it to ensure Mender Artifacts are only installed on
devices of targeted types.

It is stored in the `/var/lib/mender/device_type` file by default (can be
changed in config), as part of the Client's persistent data (as it doesn't
change with updates) and uses the following format:

```
device_type=THE_DEVICE_TYPE
```

Although the device type can be an arbitrary string, it is better to avoid
spaces and special characters that can be (mis-)interpreted in shell commands.

If the file doesn't exist, it needs to be created and populated according to the
above format. If it exists, please check that its contents are correct, or
update it.


Configuration file(s)
----------------------

The Mender Client loads two configuration files of JSON format:
`/etc/mender/mender.conf` and `/var/lib/mender/mender.conf`. As their locations
suggest, the latter one is for persistent configuration and the former one is
part of the system configuration, expected to change with updates. And the merge
logic reflects that -- the `/var/lib/mender/mender.conf` file is loaded first
(if present) and then `/etc/mender/mender.conf` is loaded (if present),
potentially overriding the configuration loaded before. In fact, the first
configuration playing its role in this merge logic is the built-in configuration
of the Mender Client itself, providing default values.

The very minimal configuration file typically needs to at least specify two
key-value pairs:

```
{
  "ServerURL": "MENDER_SERVER_URL",
  "TenantToken": "MENDER_TENANT_TOKEN"
}
```

for the Mender Server URL and Tenant Token, respectively. Mender Server
deployments without multi-tenancy don't require the the Tenant Token, of course.

The other key-value pairs that are often specified include:

```
  "UpdatePollIntervalSeconds": INTEGER_NUMBER,
  "InventoryPollIntervalSeconds": INTEGER_NUMBER,
  "RetryPollIntervalSeconds": INTEGER_NUMBER,
```

to specify intervals at which the Mender Client should talk to the Mender
Server. **Make sure to respect the JSON format's sensitivity to commas when
editing the config files.**

See the [full
documentation](https://docs.mender.io/client-installation/configuration/configuration-options)
for the full list of configuration options and their exact meaning/effects and
the [full Client Installation docs](https://docs.mender.io/client-installation)
for further information about Mender Client installation and setup in general.


Start on boot
--------------

It is critical to make sure the Mender Client processes are running on the
system when the system boots. Otherwise a spontaneous reboot results in a device
not reconnecting to the Mender Server making OTA impossible.

Various mechanisms can be utilized, depending on the Operating System (OS) and
the boot process.


### Linux

The most common solution on modern Linux systems is _systemd_. The Mender Client
works with the fact and its installations for Linux systems ship with systemd
unit files -- one or two `.service` files. The `mender-updated.service` file
defines a service running `mender-update` as a daemon and thus it needs to be
enabled with:

```
systemctl enable mender-updated
```

In order to start the daemon once/immediately use:

```
systemctl start mender-updated
```

The service also makes sure that the `mender-update` daemon is restarted in case
it stopped unexpectedly, giving it a chance to connect to the Mender Server and
deploy an update resolving such unexpected failures.

In case the system is not running systemd as the init/PID 1 process, there is
some other mechanism for starting processes on boot. Consult the documentation
to find the right place where the line:

```
mender-update --log-file /var/log/mender.log daemon
```

can be added, potentially with an `&` at the end if it needs to run in
background. Ideally, such a place should also ensure the same command is run
again in case the previous run terminated unexpectedly.


### QNX

QNX systems have two mechanisms for starting processes on boot:

- the System Launch and Monitor (SLM), and
- the `post_startup.sh` script.

SLM provides a nice mechanism for running processes in a similar way _systemd_
does on Linux and is thus the best possible option covering both starting the
Mender Client on boot and making sure the process is restarted in case of an
unexpected termination. **However**, SLM configuration may only be possible
during system build. See the [QNX
documentation](https://www.qnx.com/developers/docs/qnxeverywhere/com.qnx.doc.target_images/topic/cti/snippets.html)
on how to do that and how an [SLM application can be
defined](https://www.qnx.com/developers/docs/8.0/com.qnx.doc.neutrino.utilities/topic/s/slm.html).

The `post_startup.sh` script provides a simple mechanism for starting processes
on boot and the Mender Client can be included by inserting the following line:

```
mender-update --log-file /var/log/mender.log daemon &
```

into the script, **before** the `exit 0` line. However, this solution is not
robust because if the process terminates unexpectedly, nothing will restart it.
