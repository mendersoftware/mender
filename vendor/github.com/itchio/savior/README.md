# savior

![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)
[![Build Status](https://travis-ci.org/itchio/savior.svg?branch=master)](https://travis-ci.org/itchio/savior)
[![codecov](https://codecov.io/gh/itchio/savior/branch/master/graph/badge.svg)](https://codecov.io/gh/itchio/savior)
[![Go Report Card](https://goreportcard.com/badge/github.com/itchio/savior)](https://goreportcard.com/report/github.com/itchio/savior)

savior is an optimistic attempt at providing an abstract layer over
various compression formats (like deflate, gzip, bzip2) and archive formats (like zip, tar, etc.)
all while providing reasonably good save/resume support.

## Concepts

There are two main interfaces in savior: **Sources** and **Extractors**.

### Sources

A `savior.Source` represents a data stream that can be read from start to end.

For example, a source might be:

  * An HTTP(S) resource on a server
  * A file on disk
  * A buffer in memory
  * Another source being decompressed from FLATE, gzip, or bzip2

savior ships with `seeksource`, which covers the former (in combination with
[htfs](https://godoc.org/github.com/itchio/httpkit/htfs)), and
`flatesource`, `gzipsource`, `bzip2source`, which cover the latter.

A source's size doesn't need to be known in advance, although sources can optionally
implement a `Progress()` method that returns a `float64` in [0,1] — indicating how
much of the stream has been consumed.

Random access is not required of sources, but saving and resuming is.

Before using a source, the `Resume()` method should always be called:

  * If it's a fresh source, a nil checkpoint should be passed to Resume. This
  indicates "start from the beginning"
  * If we're resuming mid-stream, a `*SourceCheckpoint` is passed. The source
  shall then try to resume using that checkpoint information. Whether it fails
  or not, the offset it returns should be valid. (TL;DR if it fails, just return a 0 offset)

`*SourceCheckpoint` are typically saved to non-volatile storage - the test suite
ensures that they can be encoded/decoded via [encoding/gob](https://godoc.org/encoding/gob).

Decompressing sources like `flatesource` and `bzip2source` can typically only checkpoint
on a block boundary. For that reason, it's legal for sources to return a nil `*SourceCheckpoint`
from the `Save()` method. It just means that if you stop reading there and resume later, it'll
start over from the beginning.

To account for the fact that sources may save an earlier position than you needed, the
`DiscardByRead` function is exposed, letting you advance by a number of bytes to resume
reading exactly where you needed.

Note: `flatesource`, `gzipsource` and `bzip2source` are all implemented on top of forks
of golang's flate, gzip and bzip2 extractors, which can be found at [itchio/kompress](https://github.com/itchio/kompress)

### Extractors

Extractors abstract over archive formats, like `.tar` and `.zip`, which may contain
multiple entries (directories, files, symlinks).

It's not easy to find a common interface between those, since the `.zip` format
knows about all entries and their sizes in advance, whereas the `.tar` format has
no dictionary, entries are discovered one by one as the archive is extracted.

Extractors all have their own, specific `New()` method, taking whatever arguments they
need to read and extract an archive.

However, they share a few common methods:

  * `Resume` asks an extractor to start work, either from scratch or from a checkpoint.
    It returns an `ExtractorResult`, which contains a list of `*Entry` - all extractors
    are able to return the complete contents of the archive once it is fully extracted.
  * `SetSaveConsumer` sets a `SaveConsumer` for the extractor, which it'll use whenever
    it's ready to save (and `SaveConsumer.ShouldSave` returns true). Extractor state are
    saved as `*ExtractorCheckpoint`, which are guaranteed to be encodable via
    [encoding/gob](https://godoc.org/encoding/gob). `SaveConsumer` implementations can also
    stop decompression by returning `AfterSaveStop` from `Save()`.
  * `SetConsumer` sets a `*state.Consumer` for the extractor, which it'll use to send
    log messages and emit progress info (a `float64` in a [0,1] range).
  * `Features` returns the set of features supported by an extractor, including how
    good its resume support is (non-existent, between entries, or mid-entries), whether
    it supports preallocation, etc.

Extractors can use sources internally, for example:

  * A `gzipsource` can be passed to `tarextractor` to extract a `.tar.gz` file. The
    `tarextractor` will checkpoint any underlying source, so it doesn't need to know
    that the whole tar is in fact read from a gzip stream.
  * The `zipextractor` will use a `flatesource` for entries compressed with the `Deflate`
    method - this allows it to checkpoint mid-entry.

Note: `tarextractor` and `zipextractor` are implemented on top of forks of golang's
zip and tar archive handlers, which can be found at [itchio/arkive](https://github.com/itchio/arkive).

Note: extractors are not responsible for closing sinks - the sinks are created and closed
by the caller itself.

### Sinks

A `Sink` is typically what an extractor extracts "to". In the simplest case, it's a
`FolderSink`, which writes directly to the filesystem. However, other implementations
exist, such as `checker.Sink`, used in test to extract in-memory and validate the decompressed
data against a reference set.

`FolderSink` is opinionated — in particular, it:

  * Writes symlinks as text files on Windows
    * Many versions of Windows support junctions, but they have different semantics, so
      they're not used
    * Many versions of Windows support actual symlinks, but they require Administrator
      privileges to create, so they're not used
    * Recent builds of Windows 10 support creating symlinks without Administrator privileges,
      but that's hardly the common denominator, so they're not used
    * Writing symlinks as text files with the os.SymlinkMode permission matches the way
      they're stored in .zip files, or various *nix filesystems
  * Always creates necessary parent folders (with 0755)
    * If `GetWriter()` is called for a file entry with CanonicalPath `a/b/c`,
    the `a/` and `a/b/` folders will be created
  * Does whatever it take to make sure the filesystem entry is of the right type
    * If `GetWriter()` is called for a file entry with CanonicalPath `plugin`,
    but `plugin` is currently a folder or symlink on disk, it will be removed
    first and re-created as a file
  * Adjusts permissions so that they're at least `0644` (or more permissive).
    This avoids creating files which we don't have permission to erase or overwrite later.
  * Truncates file to `entry.UncompressedSize` when `Preallocate()` is called, but not when
    `GetWriter()` is called, so that archive formats which have a zero UncompressedSize still
    work when resuming mid-entry.

### License

savior is released under the MIT license, see the `LICENSE` file in this repository.
