/*
Package lmdb provides bindings to the lmdb C API.  The package bindings are
fairly low level and are designed to provide a minimal interface that prevents
misuse to a reasonable extent.  When in doubt refer to the C documentation as a
reference.

	http://symas.com/mdb/doc/group__mdb.html

Environment

An LMDB environment holds named databases (key-value stores).  An environment
is represented as one file on the filesystem (though often a corresponding lock
file exists).

LMDB recommends setting an environment's size as large as possible at the time
of creation.  On filesystems that support sparse files this should not
adversely affect disk usage.  Resizing an environment is possible but must be
handled with care when concurrent access is involved.

Databases

A database in an LMDB environment is an ordered key-value store that holds
arbitrary binary data.  Typically the keys are unique but duplicate keys may be
allowed (DupSort), in which case the values for each duplicate key are ordered.

A single LMDB environment can have multiple named databases.  But there is also
a 'root' (unnamed) database that can be used to store data.  Use caution
storing data in the root database when named databases are in use.  The root
database serves as an index for named databases.

A database is referenced by an opaque handle known as its DBI which must be
opened inside a transaction with the OpenDBI or OpenRoot methods.  DBIs may be
closed but it is not required.  Typically, applications acquire handles for all
their databases immediately after opening an environment and retain them for
the lifetime of the process.

Transactions

View (readonly) transactions in LMDB operate on a snapshot of the database at
the time the transaction began.  The number of simultaneously active view
transactions is bounded and configured when the environment is initialized.

Update (read-write) transactions are serialized in LMDB.  Attempts to create
update transactions block until a lock may be obtained.  Update transactions
can create subtransactions which may be rolled back independently from their
parent.

The lmdb package supplies managed and unmanaged transactions. Managed
transactions do not require explicit calling of Abort/Commit and are provided
through the Env methods Update, View, and RunTxn.  The BeginTxn method on Env
creates an unmanaged transaction but its use is not advised in most
applications.
*/
package lmdb

/*
#cgo CFLAGS: -pthread -W -Wall -Wno-unused-parameter -Wno-format-extra-args -Wbad-function-cast -O2 -g
#cgo linux,pwritev CFLAGS: -DMDB_USE_PWRITEV

#include "lmdb.h"
*/
import "C"

// Version return the major, minor, and patch version numbers of the LMDB C
// library and a string representation of the version.
//
// See mdb_version.
func Version() (major, minor, patch int, s string) {
	var maj, min, pat C.int
	verstr := C.mdb_version(&maj, &min, &pat)
	return int(maj), int(min), int(pat), C.GoString(verstr)
}

// VersionString returns a string representation of the LMDB C library version.
//
// See mdb_version.
func VersionString() string {
	var maj, min, pat C.int
	verstr := C.mdb_version(&maj, &min, &pat)
	return C.GoString(verstr)
}

func cbool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
