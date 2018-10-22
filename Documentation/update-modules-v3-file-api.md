Update modules v3 protocol
==========================

Update modules are executables that are placed in `/usr/lib/mender/modules/v3`
directory, where `v3` is a reference to the version of the protocol. Mender will
look in the directory with the same version as the version of the artifact being
processed.


States and execution flow
-------------------------

Update modules are modelled after the same execution flow as state scripts, and
consists of the following states:

* `Download`
* `ArtifactInstall`
* `ArtifactReboot`
* `ArtifactCommit`
* `Cleanup`

These all execute in the listed order, given that there are no errors. There are
also some additional error states:

* `ArtifactRollback`
* `ArtifactRollbackReboot`
* `ArtifactFailure`

`ArtifactRollback` executes whenever:

* `ArtifactInstall` has executed successfully
* `ArtifactReboot` or `ArtifactCommit` fails

`ArtifactRollbackReboot` executes whenever:

* `ArtifactReboot` has executed successfully
* `ArtifactRollback` has executed

`ArtifactFailure` executes whenever:

* Either of `ArtifactInstall`, `ArtifactReboot` or `ArtifactCommit` has
  failed
* Executes after `ArtifactRollback` and `ArtifactRollbackReboot`, if they
  execute at all

`Cleanup` executes unconditionally at the end of all the other states,
regardless of all outcomes. It is the only additional state that executes if
`Download` fails.


File API
--------

This document describes the file layout of the directory that is given to update
modules when they launch. This directory will be pre-filled with certain pieces
of information from the client, and must be used by update modules.

```
-<DIRECTORY>
  |
  +---artifact_name
  |
  +---device_type
  |
  +---header
  |    |
  |    +---header-info
  |    |
  |    +---files
  |    |
  |    +---type-info
  |    |
  |    `---meta-data
  |
  `----header-augment
  |    |
  |    +---header-info
  |    |
  |    +---files
  |    |
  |    +---type-info
  |    |
  |    `---meta-data
  |
  `---tmp
```

In addition it may contain one of these two trees, depending on context. The
"streams tree":

```
-<DIRECTORY>
  |
  +---streams-list
  |
  +---streams
  |    |
  |    +---<STREAM-1>
  |    +---<STREAM-2>
  |    `---<STREAM-n> ...
  |
  `---streams-augment
       |
       +---<STREAM-1>
       +---<STREAM-2>
       `---<STREAM-n> ...
```

or the "files tree":

```
-<DIRECTORY>
  |
  +---files
  |    |
  |    +---<FILE-1>
  |    +---<FILE-2>
  |    `---<FILE-n> ...
  |
  `---files-augment
       |
       +---<FILE-1>
       +---<FILE-2>
       `---<FILE-n> ...
```

### `artifact_name` and `device_type`

`artifact_name` and `device_type` correspond to the currently installed artifact
name and the device type which is normally stored at
`/var/lib/mender/device_type`. They contain pure values, unlike the original
files that contain key/value pairs.

### `header`

The `header` directory contains the verbatim headers from the `header.tar.gz`
header file of the artifact. One artifact can contain payloads for several
update module, so the three files `files`, `type-info` and `meta-data` are taken
from the indexed subfolder currently being processed by Mender.

### `header-augment`

The `header-augment` directory functions exactly as the `header` directory, but
is taken from the `header-augment.tar.gz` file from the artifact.

**Warning:** Be very careful with using contents from `header-augment` because
it contains unsigned data. Unless you specifically need unsigned data in your
update module (for example for a binary diff that depends on the device it is
targeted against), it is advised not to use the `header-augment` directory.

### `tmp`

`tmp` is merely a convenience directory that the update module can use for
temporary storage. It is guaranteed to exist, to be empty, and to be cleaned up
after the update has completed (or failed). The module is not obligated to use
this directory, it can also use other, more suited locations if desirable, but
then the module must clean it up by implementing the `Cleanup` state.

### Streams tree

The stream tree only exists during the `Download` state, which is when the
download is still being streamed from the server. If the update module doesn't
want to perform its own streaming, and simply wishes to save the streams to
files, it can simply do nothing in the `Download` state, and Mender will
automatically save the file in the "files tree".

`streams-list` contains a list of streams inside the `streams` and
`streams-augment` directories. The path will have exactly two components: which
directory it is in, and the name of the pipe which is used to stream the
content. For example:

```
streams/pkg-file.deb
streams-augment/patch.diff
```

Each entry is a named pipe which can be used to stream the content from the
update. The stream is taken from the `data/nnnn.tar.gz` payload that corresponds
to the indexed subfolder being processed by Mender, just like the header.

**Important:** The contents must be read in the same order that entries appear
in the `streams-list` file, and each entry can only be read once. If this is not
followed the update module may hang.

**Important:** An update module must not install the update in the final
location during the streaming stage, because checksums are not verified until
after the streaming stage is over. If it must be streamed to the final location
(such as for example a partition), it should be stored in an inactive state, so
that it is not accidentally used, and then be activated in the
`ArtifactInstall` stage. Failure to do so can mean that the update module will
be vulnerable to security attacks.

**Important:** The `streams-augment` directory contains unsigned data. Unless
you specifically need unsigned data in your update module (for example for a
binary diff that depends on the device it is targeted against), it is advised
not to use entries in the `streams-augment` directory. In order to avoid
accidental inclusion of unsigned data, if the update contains any entries in
`streams-augment`, the `streams-list` will be called `augmented-streams-list`
instead of `streams-list`. Entries in `streams` are always checked for
signatures.

### File tree

The file tree only exists in the `ArtifactInstall` and later states, and only
if the streams were **not** consumed during the `Download` state. In this case
Mender will download the streams automatically and put them in the file tree.

The `files` directory contains the payloads from the artifact, and is taken from
the `data/nnnn.tar.gz` payload that corresponds to the indexed subfolder being
processed by Mender, just like the header.

**Important:** The `files-augment` directory contains unsigned data. Unless you
specifically need unsigned data in your update module (for example for a binary
diff that depends on the device it is targeted against), it is advised not to
use entries in the `files-augment` directory.


Execution
---------

The update module is called once for each state that occurs, with the working
directory set to the directory where the File API resides. It is called with
exactly two arguments: The current state, and the absolute path of the File API
location.

Returning any non-zero value in the update module triggers a failure, and will
invoke the relevant failure states.
