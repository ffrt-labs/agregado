package logging

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skipDirs are trees that aren't Go we own: the Cloudflare worker's npm tree,
// the build output, and fixture files that are never compiled into the module.
var skipDirs = map[string]bool{
	"node_modules": true,
	"bin":          true,
	".git":         true,
	"testdata":     true,
}

// moduleRoot walks up from the working directory to the directory holding
// go.mod. Found rather than hardcoded as "../..": a relative depth encodes
// where this file happens to live, so moving the package would silently
// shrink what gets scanned instead of failing.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the working directory")
		}
		dir = parent
	}
}

// TestNoUnstructuredLogging is Phase 22's "no remaining fmt.Print*/log.Print*
// calls, verified via grep" acceptance criterion turned into a test, so the
// sweep can't quietly rot back. It parses every non-test .go file in the module
// and fails on any call into the stdlib `log` package or `fmt.Print*`, plus the
// `print`/`println` builtins.
//
// Test files are exempt: a t.Log-adjacent fmt.Printf in a test never reaches the
// collector, and pinning test diagnostics is not what the criterion is about.
func TestNoUnstructuredLogging(t *testing.T) {
	fset := token.NewFileSet()
	root := moduleRoot(t)
	var violations []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			// Report and keep going: one unparseable file must not abort the
			// scan and disguise itself as a walk failure, which would leave
			// every other file silently unchecked.
			t.Errorf("parsing %s: %v", path, err)
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name, bad := unstructuredCall(call); bad {
				violations = append(violations, fset.Position(call.Pos()).String()+": "+name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	for _, v := range violations {
		t.Errorf("unstructured logging call — use slog with a \"component\" field instead\n\t%s", v)
	}
}

// unstructuredCall reports whether a call is one of the unstructured logging
// forms the sweep removed, and what to name it in the failure. It matches on
// the source-level identifier (`log`, `fmt`) rather than resolved types, which
// is the same thing the grep in the acceptance criteria did — good enough, and
// it keeps the test free of go/types.
//
// Matching the whole `log` package rather than just `log.Print*` is what makes
// the source-level shortcut hold: the only ways to obtain a *log.Logger are
// log.New and log.Default, so a `l.Printf` this can't see by type is always
// downstream of a construction call it can see by name.
func unstructuredCall(call *ast.CallExpr) (string, bool) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// The print/println builtins: never shadowed in this module, and a
		// shadowed one is a call to something else that reads just as badly.
		if fn.Name == "print" || fn.Name == "println" {
			return fn.Name, true
		}
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		switch pkg.Name {
		case "log":
			// The stdlib log package, whole. log/slog is imported as `slog`,
			// so this never catches the structured path.
			return "log." + fn.Sel.Name, true
		case "fmt":
			if strings.HasPrefix(fn.Sel.Name, "Print") {
				return "fmt." + fn.Sel.Name, true
			}
			// Fprint* is only a violation when it writes to the process
			// streams — the same unstructured output by a longer route.
			// Writing to a buffer, a response body, or a file is not logging.
			if strings.HasPrefix(fn.Sel.Name, "Fprint") && len(call.Args) > 0 && writesToProcessStream(call.Args[0]) {
				return "fmt." + fn.Sel.Name, true
			}
		}
	}
	return "", false
}

// writesToProcessStream reports whether an io.Writer argument is os.Stdout or
// os.Stderr — the two destinations that make an fmt.Fprint* call a log line
// rather than ordinary output.
func writesToProcessStream(arg ast.Expr) bool {
	sel, ok := arg.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "os" {
		return false
	}
	return sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr"
}
