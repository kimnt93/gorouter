package routes

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestJSONHandlersUseSharedResponseBuilder(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	handlerDirectory := filepath.Join(filepath.Dir(sourceFile), "..", "handlers")
	files, err := filepath.Glob(filepath.Join(handlerDirectory, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	for _, file := range files {
		if filepath.Ext(file) != ".go" || filepath.Base(file) == "ui.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}
		tree, parseErr := parser.ParseFile(set, file, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		ast.Inspect(tree, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "JSON" {
				if receiver, isIdentifier := selector.X.(*ast.Ident); isIdentifier && receiver.Name == "c" {
					t.Errorf("%s:%d calls c.JSON directly", file, set.Position(call.Pos()).Line)
				}
			}
			return true
		})
	}
}
