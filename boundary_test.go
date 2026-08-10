package aiostreams_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestModuleImportsOnlyPublishedContracts is the module boundary made
// executable: this module must use only the published *contract* modules — the
// SDK (mosaic-sdk) and the shared SDUI contract (mosaic-sdui, which a module
// contributes its settings UI with, sdk#4) — and the standard library.
//
// It is a separate Go module, so Go itself already rejects a Platform-internal
// import; this parse keeps the intent explicit and catches a third-party
// dependency creeping in (sdk#1, platform#12, contracts#3).
//
// It also catches the specific temptation this module has and the others do not:
// `module-stremio-addons` already speaks the same wire protocol, and importing
// its client would look like avoiding duplication. It is not available to import
// — a module may not depend on another module — and the small amount of shared
// shape here is the price of each module being an anti-corruption layer for its
// own upstream (module-stremio-addons#2).
func TestModuleImportsOnlyPublishedContracts(t *testing.T) {
	const (
		sdkPrefix      = "github.com/mosaic-media/sdk/"
		sduiPrefix     = "github.com/mosaic-media/contracts/"
		platformPrefix = "github.com/mosaic-media/platform/"
		modulePrefix   = "github.com/mosaic-media/module-"
	)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++

		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", name, err)
			}
			switch {
			// Standard-library imports have no dot in their first segment.
			case !strings.Contains(strings.SplitN(path, "/", 2)[0], "."):
			case strings.HasPrefix(path, sdkPrefix):
				// The published SDK — the primary contract a module builds against.
			case strings.HasPrefix(path, sduiPrefix):
				// The shared SDUI contract — a module builds its own settings UI with
				// the producer binding (sdk#4, contracts#3).
			case strings.HasPrefix(path, platformPrefix):
				t.Errorf("%s imports private Platform package %q; a module may import only the SDK", name, path)
			case strings.HasPrefix(path, modulePrefix):
				t.Errorf("%s imports another module %q; modules compose through the Platform, never with each other", name, path)
			default:
				t.Errorf("%s imports third-party package %q; this module may use only the SDK, SDUI and the standard library", name, path)
			}
		}
	}

	if checked == 0 {
		t.Fatal("no non-test source files were checked; the boundary test is not looking at anything")
	}
}
