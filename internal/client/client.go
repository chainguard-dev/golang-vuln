// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package client provides an interface for accessing vulnerability
// databases, via either HTTP or local filesystem access.
//
// The protocol is described at https://go.dev/security/vulndb/#protocol.
//
// The expected database layout is the same for both HTTP and local
// databases. The database index is located at the root of the
// database, and contains a list of all of the vulnerable modules
// documented in the database and the time the most recent vulnerability
// was added. The index file is called index.json, and has the
// following format:
//
//	map[string]time.Time (DBIndex)
//
// Each vulnerable module is represented by an individual JSON file
// which contains all of the vulnerabilities in that module. The path
// for each module file is simply the import path of the module.
// For example, vulnerabilities in golang.org/x/crypto are contained in the
// golang.org/x/crypto.json file. The per-module JSON files contain a slice of
// https://pkg.go.dev/golang.org/x/vuln/internal/osv#Entry.
//
// A single client.Client can be used to access multiple vulnerability
// databases. When looking up vulnerable modules, each database is
// consulted, and results are merged together.
package client

import (
	"context"
	"time"

	"golang.org/x/vuln/internal/osv"
)

// Client interface for fetching vulnerabilities based on module path or ID.
type Client interface {
	// ByModule returns the entries that affect the given module path.
	// It returns (nil, nil) if there are none.
	ByModule(context.Context, string) ([]*osv.Entry, error)

	// LastModifiedTime returns the time that the database was last modified.
	// It can be used by tools that periodically check for vulnerabilities
	// to avoid repeating work.
	LastModifiedTime(context.Context) (time.Time, error)
}
