//go:build sqlite
// +build sqlite

package pop

import (
	_ "modernc.org/sqlite" // Load SQLite3 driver
)
