package trace

import (
	"testing"

	"bennypowers.dev/mappa/packagejson"
)

func TestTracerBuildersPreserveFields(t *testing.T) {
	pkg := &packagejson.PackageJSON{Name: "self"}
	tr := NewTracer(nil, "/root").
		WithLogger(nil).
		WithNodeModules("/root/node_modules").
		WithConditions([]string{"browser"}).
		WithSelfPackage(pkg, "/root")

	if tr.rootDir != "/root" {
		t.Errorf("rootDir lost: %q", tr.rootDir)
	}
	if tr.nodeModulesPath != "/root/node_modules" {
		t.Errorf("nodeModulesPath lost: %q", tr.nodeModulesPath)
	}
	if !tr.followBare {
		t.Error("followBare lost")
	}
	if len(tr.conditions) != 1 || tr.conditions[0] != "browser" {
		t.Errorf("conditions lost: %v", tr.conditions)
	}
	if tr.selfPkg != pkg || tr.selfPkgPath != "/root" {
		t.Error("self package lost")
	}
}
