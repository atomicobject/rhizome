// Compatibility shim for vendored sqlite-vec cgo builds.
//
// The sqlite-vec amalgamation expects sqlite3.h, but this repository vendors
// mattn/go-sqlite3's amalgamation as sqlite3-binding.h. Keep sqlite-vec using
// the same SQLite headers that go-sqlite3 compiles against, including Windows
// CI where a system sqlite3.h may not be installed.
#include "../../../mattn/go-sqlite3/sqlite3-binding.h"
