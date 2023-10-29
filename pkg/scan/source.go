// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package scan

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"golang.org/x/tools/go/packages"
	"golang.org/x/vuln/pkg"
	"golang.org/x/vuln/pkg/client"
	"golang.org/x/vuln/pkg/govulncheck"
	"golang.org/x/vuln/pkg/vulncheck"
)

// runSource reports vulnerabilities that affect the analyzed packages.
//
// Vulnerabilities can be called (affecting the package, because a vulnerable
// symbol is actually exercised) or just imported by the package
// (likely having a non-affecting outcome).
func runSource(ctx context.Context, handler govulncheck.Handler, cfg *config, client *client.Client, dir string) error {
	if len(cfg.patterns) == 0 {
		return nil
	}
	var pkgs []*packages.Package
	graph := vulncheck.NewPackageGraph(cfg.GoVersion)
	pkgConfig := &packages.Config{
		Dir:   dir,
		Tests: cfg.test,
		Env:   cfg.env,
	}
	pkgs, err := graph.LoadPackages(pkgConfig, cfg.tags, cfg.patterns)
	if err != nil {
		// Try to provide a meaningful and actionable error message.
		if !fileExists(filepath.Join(dir, "go.mod")) {
			return fmt.Errorf("govulncheck: %v", errNoGoMod)
		}
		if isGoVersionMismatchError(err) {
			return fmt.Errorf("govulncheck: %v\n\n%v", errGoVersionMismatch, err)
		}
		return fmt.Errorf("govulncheck: loading packages: %w", err)
	}
	if err := handler.Progress(sourceProgressMessage(pkgs)); err != nil {
		return err
	}
	vr, err := vulncheck.Source(ctx, handler, pkgs, &cfg.Config, client, graph)
	if err != nil {
		return err
	}
	callStacks := vulncheck.CallStacks(vr)
	return emitCalledVulns(handler, callStacks)
}

func emitCalledVulns(handler govulncheck.Handler, callstacks map[*vulncheck.Vuln]vulncheck.CallStack) error {
	var vulns []*vulncheck.Vuln

	for v := range callstacks {
		vulns = append(vulns, v)
	}

	sort.SliceStable(vulns, func(i, j int) bool {
		return vulns[i].Symbol < vulns[j].Symbol
	})

	for _, vuln := range vulns {
		stack := callstacks[vuln]
		if stack == nil {
			continue
		}
		fixed := vulncheck.FixedVersion(vulncheck.ModPath(vuln.ImportSink.Module), vulncheck.ModVersion(vuln.ImportSink.Module), vuln.OSV.Affected)
		handler.Finding(&govulncheck.Finding{
			OSV:          vuln.OSV.ID,
			FixedVersion: fixed,
			Trace:        tracefromEntries(stack),
		})
	}
	return nil
}

// tracefromEntries creates a sequence of
// frames from vcs. Position of a Frame is the
// call position of the corresponding stack entry.
func tracefromEntries(vcs vulncheck.CallStack) []*govulncheck.Frame {
	var frames []*govulncheck.Frame
	for i := len(vcs) - 1; i >= 0; i-- {
		e := vcs[i]
		fr := frameFromPackage(e.Function.Package)
		fr.Function = e.Function.Name
		fr.Receiver = e.Function.Receiver()
		if e.Call == nil || e.Call.Pos == nil {
			fr.Position = nil
		} else {
			fr.Position = &govulncheck.Position{
				Filename: e.Call.Pos.Filename,
				Offset:   e.Call.Pos.Offset,
				Line:     e.Call.Pos.Line,
				Column:   e.Call.Pos.Column,
			}
		}
		frames = append(frames, fr)
	}
	return frames
}

func frameFromPackage(pkg *packages.Package) *govulncheck.Frame {
	fr := &govulncheck.Frame{}
	if pkg != nil {
		fr.Module = pkg.Module.Path
		fr.Version = pkg.Module.Version
		fr.Package = pkg.PkgPath
	}
	if pkg.Module.Replace != nil {
		fr.Module = pkg.Module.Replace.Path
		fr.Version = pkg.Module.Replace.Version
	}
	return fr
}

// sourceProgressMessage returns a string of the form
//
//	"Scanning your code and P packages across M dependent modules for known vulnerabilities..."
//
// P is the number of strictly dependent packages of
// topPkgs and Y is the number of their modules.
func sourceProgressMessage(topPkgs []*packages.Package) *govulncheck.Progress {
	pkgs, mods := depPkgsAndMods(topPkgs)

	pkgsPhrase := fmt.Sprintf("%d package", pkgs)
	if pkgs != 1 {
		pkgsPhrase += "s"
	}

	modsPhrase := fmt.Sprintf("%d dependent module", mods)
	if mods != 1 {
		modsPhrase += "s"
	}

	msg := fmt.Sprintf("Scanning your code and %s across %s for known vulnerabilities...", pkgsPhrase, modsPhrase)
	return &govulncheck.Progress{Message: msg}
}

// depPkgsAndMods returns the number of packages that
// topPkgs depend on and the number of their modules.
func depPkgsAndMods(topPkgs []*packages.Package) (int, int) {
	tops := make(map[string]bool)
	depPkgs := make(map[string]bool)
	depMods := make(map[string]bool)

	for _, t := range topPkgs {
		tops[t.PkgPath] = true
	}

	var visit func(*packages.Package, bool)
	visit = func(p *packages.Package, top bool) {
		path := p.PkgPath
		if depPkgs[path] {
			return
		}
		if tops[path] && !top {
			// A top package that is a dependency
			// will not be in depPkgs, so we skip
			// reiterating on it here.
			return
		}

		// We don't count a top-level package as
		// a dependency even when they are used
		// as a dependent package.
		if !tops[path] {
			depPkgs[path] = true
			if p.Module != nil &&
				p.Module.Path != pkg.GoStdModulePath && // no module for stdlib
				p.Module.Path != pkg.UnknownModulePath { // no module for unknown
				depMods[p.Module.Path] = true
			}
		}

		for _, d := range p.Imports {
			visit(d, false)
		}
	}

	for _, t := range topPkgs {
		visit(t, true)
	}

	return len(depPkgs), len(depMods)
}
