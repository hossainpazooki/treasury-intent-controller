// Package contractcheck mechanically enforces CONTRACT.md: the production
// import adjacency (§7), the role vocabulary (§1), and core neutrality
// (§9). It is
// test-only — it ships no production code, and no package imports it.
//
// P2 ("artifacts are the only crossings") is a deployment claim; what this
// package makes mechanical is its gate-side precondition: the intent interface
// (the four HTTP routes plus the durable feed) is the ONLY surface, because
// the intra-repo import graph matches the pinned adjacency exactly. Any new
// edge, any new core package outside core/internal/ (other than
// core/cmd/server and plane), or any dropped edge fails here.
//
// LAYERING (§7, 2026-08-05 ruling): the core is a minimal SDK — core/ plus
// plane/ (the boundary artifact, verification only). Everything else at the
// top level is an APPLICATION tree built on the SDK. Core packages are pinned
// exactly, by table. Application trees are pinned by RULE, never by name: the
// SDK's checks carry no application vocabulary (that is core-neutrality's
// job to keep true).
package contractcheck

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const modulePrefix = "github.com/hossainpazooki/intent-plane/"

// allowedProd pins the production (non-_test.go) import adjacency of the CORE,
// intra-module edges only. Keys are the complete set of core Go package
// directories: a core package missing here — or present here but missing on
// disk — is a boundary change and must be made deliberately, in CONTRACT.md
// first. Application packages never appear in this table.
var allowedProd = map[string][]string{
	"core/cmd/server":             {"core/internal/durable", "core/internal/gate", "core/internal/idempotency", "core/internal/intent", "core/internal/scoring", "plane"},
	"plane":                       {},
	"core/internal/adapter":       {"core/internal/intent"},
	"core/internal/audit":         {},
	"core/internal/contractcheck": {},
	"core/internal/durable":       {},
	"core/internal/gate":          {"core/internal/audit", "core/internal/durable", "core/internal/idempotency", "core/internal/intent", "core/internal/lifecycle", "core/internal/scoring"},
	"core/internal/idempotency":   {"core/internal/intent"},
	"core/internal/intent":        {},
	"core/internal/lifecycle":     {},
	"core/internal/scoring":       {"core/internal/intent"},
}

// allowedTestExtra pins edges that exist ONLY in _test.go files, beyond the
// production adjacency. gate -> adapter is the sanctioned one: the acceptance
// suite drives the TEST-ONLY reference adapter (CONTRACT.md §7.2). Core tests
// sign fixtures with TEST-LOCAL helpers — a core test importing an
// application tree is a layering violation, not a sanctioned extra.
var allowedTestExtra = map[string][]string{
	"core/cmd/server":             {"core/internal/lifecycle"},
	"plane":                       {},
	"core/internal/adapter":       {},
	"core/internal/audit":         {},
	"core/internal/durable":       {},
	"core/internal/gate":          {"core/internal/adapter", "core/internal/lifecycle"},
	"core/internal/idempotency":   {},
	"core/internal/intent":        {},
	"core/internal/lifecycle":     {},
	"core/internal/scoring":       {},
	"core/internal/contractcheck": {},
}

// isCoreDir reports whether a repo-relative package dir belongs to the SDK
// core (core/... or the plane artifact). Everything else is application land.
func isCoreDir(d string) bool {
	return d == "plane" || strings.HasPrefix(d, "plane/") || d == "core" || strings.HasPrefix(d, "core/")
}

// appTree returns the top-level tree name of an application package dir.
func appTree(d string) string {
	if i := strings.Index(d, "/"); i >= 0 {
		return d[:i]
	}
	return d
}

// repoRoot walks up from the test's CWD to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// skipDir reports directories the walk never descends into: VCS, build output,
// the Python service, and anything hidden.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "bin" || name == "scorer" || name == "data" || name == "docs" || name == "contract"
}

// goPackageDirs returns every directory (repo-relative, slash-separated) that
// contains at least one .go file.
func goPackageDirs(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			seen[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// intraModuleImports parses every .go file in dir and returns the intra-module
// import sets, split into production and test-only.
func intraModuleImports(t *testing.T, root, dir string) (prod, testOnly map[string]bool) {
	t.Helper()
	prod, testOnly = map[string]bool{}, map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(root, dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s/%s: %v", dir, e.Name(), err)
		}
		into := prod
		if strings.HasSuffix(e.Name(), "_test.go") {
			into = testOnly
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, modulePrefix) {
				into[strings.TrimPrefix(p, modulePrefix)] = true
			}
		}
	}
	return prod, testOnly
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestImportBoundary pins the intent interface boundary. Core packages: the
// package SET and its import adjacency match CONTRACT.md §7 exactly.
// Application trees: pinned by rule — an application package may import only
// the plane artifact and packages within its OWN tree; nothing in the core
// may import an application package, in production OR test code. The
// verifier tree: stricter than an application — it imports nothing from the
// module outside its own tree, not even the plane artifact (§7.1: the
// examiner's independence is structural, and "verifier" is §1.1 role
// vocabulary, not application vocabulary).
func TestImportBoundary(t *testing.T) {
	root := repoRoot(t)
	dirs := goPackageDirs(t, root)

	var coreDirs, appDirs, verifierDirs []string
	for _, d := range dirs {
		switch {
		case isCoreDir(d):
			coreDirs = append(coreDirs, d)
		case appTree(d) == "verifier":
			verifierDirs = append(verifierDirs, d)
		default:
			appDirs = append(appDirs, d)
		}
	}

	// The CORE package SET is pinned: nothing appears or disappears silently.
	// The sanctioned non-internal core packages are core/cmd/server and the
	// plane artifact; everything else stays under core/internal/.
	want := sorted(func() map[string]bool {
		m := map[string]bool{}
		for k := range allowedProd {
			m[k] = true
		}
		return m
	}())
	if got := strings.Join(coreDirs, ","); got != strings.Join(want, ",") {
		t.Fatalf("core Go package set changed.\n got: %v\nwant: %v\nA new or removed core package is a boundary change: amend CONTRACT.md first.", coreDirs, want)
	}
	for _, d := range coreDirs {
		if d != "core/cmd/server" && d != "plane" && !strings.HasPrefix(d, "core/internal/") {
			t.Errorf("core package %q lives outside core/internal/ and is not core/cmd/server or plane (§7)", d)
		}
	}

	// Core adjacency: exact, table-pinned. Because the tables list only core
	// packages, any core -> application import (production or test) fails
	// here as an unsanctioned edge.
	for _, d := range coreDirs {
		prod, testOnly := intraModuleImports(t, root, d)

		wantProd := map[string]bool{}
		for _, p := range allowedProd[d] {
			wantProd[p] = true
		}
		if got, want := strings.Join(sorted(prod), ","), strings.Join(sorted(wantProd), ","); got != want {
			t.Errorf("%s: production import edges changed.\n got: [%s]\nwant: [%s]", d, got, want)
		}

		wantTest := map[string]bool{}
		for _, p := range allowedTestExtra[d] {
			wantTest[p] = true
		}
		for p := range testOnly {
			if p == d {
				// An external test package (package foo_test) importing its
				// own package is Go's black-box test idiom, not an edge.
				continue
			}
			if !wantProd[p] && !wantTest[p] {
				t.Errorf("%s: unsanctioned TEST-ONLY import edge -> %s", d, p)
			}
		}
	}

	// The verifier tree (§7.1): the independent examiner re-derives the feed's
	// commitments WITHOUT running the gate's code, so it imports nothing from
	// the module outside its own tree — not core, not plane, not an
	// application tree — in production OR test code.
	for _, d := range verifierDirs {
		prod, testOnly := intraModuleImports(t, root, d)
		for _, set := range []map[string]bool{prod, testOnly} {
			for p := range set {
				if p == d || appTree(p) == "verifier" {
					continue
				}
				t.Errorf("%s: verifier package imports %q — the verifier imports nothing from the module outside its own tree (§7.1)", d, p)
			}
		}
	}

	// Application trees: rule-pinned, name-free. Applications depend on the
	// SDK (the plane artifact), never on core internals, and never on each
	// other's trees.
	for _, d := range appDirs {
		tree := appTree(d)
		prod, testOnly := intraModuleImports(t, root, d)
		for _, set := range []map[string]bool{prod, testOnly} {
			for p := range set {
				if p == "plane" || p == d || appTree(p) == tree {
					continue
				}
				t.Errorf("%s: application package imports %q — applications may import only the plane artifact and their own tree (§7)", d, p)
			}
		}
	}
}

// TestKeyPossessionBoundary makes "holds no keys" a code-graph fact IN-REPO,
// generically: within any application tree, a package named <tree>/authority
// is a signing seat, and only <tree>/control — that application's attester
// seat — may import it in production code. The CORE (core/ and plane/) can
// import no application package at all (TestImportBoundary), so the SDK
// structurally cannot sign, publish, or activate anything: it verifies what
// applications sign, never the reverse. That "cannot" is a property of the
// import graph, not a code-review promise.
// SCOPE, honestly: this is the in-repo approximation of the claim; the
// deployment-graph half (workload identity, ROADMAP R2) is NOT established
// here and stays asserted.
func TestKeyPossessionBoundary(t *testing.T) {
	root := repoRoot(t)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, "_test.go") {
			return nil // test files may exercise a signing seat (its own tests do)
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		for _, imp := range f.Imports {
			ip := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(ip, modulePrefix) {
				continue
			}
			p := strings.TrimPrefix(ip, modulePrefix)
			if !strings.HasSuffix(p, "/authority") {
				continue
			}
			tree := appTree(p)
			if dir != p && dir != tree+"/control" {
				t.Errorf("%s imports %s: only %s/control may hold key operations (P3, in-repo half)", rel, p, tree)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
