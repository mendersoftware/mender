Automatic bootstrap Artifact
============================

The bootstrap Artifact provides a way to populate the Mender client database when it is first
initialized, since it is difficult to update the binary database from the build. Among other things,
this enables delta updates to be performed using the very first update, instead of needing to do one
rootfs update first to populate the `artifact_provides` attributes.

Properties of the bootstrap Artifact
------------------------------------

A bootstrap Artifact is an empty Artifact which provides only meta-data. Specifically, it contains
`artifact_provides` key/value pairs.

To be considered a bootstrap Artifact, the Artifact must comply with the following requirements:
* Have no `scripts`
* Have no `artifact_depends`
* Contain exactly 1 update which:
  * Include `artifact_provides`
  * Contains an empty payload, made with the `write bootstrap-artifact` argument of the
    `mender-artifact` tool

The bootstrap Artifact generated via [`meta-mender`](https://github.com/mendersoftware/meta-mender)
or [`mender-convert`](https://github.com/mendersoftware/mender-convert) contain
`rootfs-image.checksum` and `rootfs-image.version` keys, enabling therefore delta Artifacts after
installation of. However custom implementations could have other `artifact_provides` keys and it
will still be considered valid.


Automatic installation of the Artifact
--------------------------------------

On start-up, Mender checks for the existence of a bootstrap Artifact in path
`/var/lib/mender/bootstrap.mender` and installs it in order to initialize the device database. The
Artifact is not installed if the device already has a database.

This applies both for `daemon` command and `bootstrap` and `install` standalone calls.

When the Artifact is not found (and the database is empty) the database is initialized with
`artifact-name=unknown`. With errors installing the Artifact, the database is also initialized the
same way.

The bootstrap Artifact is then deleted unconditionally from the filesystem.


Signature of bootstrap Artifact
-------------------------------

For the installation of the bootstrap Artifact, the signature checking in the Mender client is
disabled. This is to save the user from signing an Artifact that he did not ask for.

If the Artifact is signed it is still considered valid and the signature is ignored.


Installations of other empty Artifacts
--------------------------------------

Mender can also understand other kinds of empty Artifacts and install them either in managed or
standalone modes. In this case the signature checking is not disabled.


How to create a bootstrap Artifact manually
-------------------------------------------

[The `mender-artifact` tool](https://github.com/mendersoftware/mender-artifact) has a convenience
command to generate bootstrap Artifacts:

```
./mender-artifact write bootstrap-artifact \
  --device-type my-device \
  --artifact-name my-bootstrap-artifact \
  --provides some-key:some-value
```
