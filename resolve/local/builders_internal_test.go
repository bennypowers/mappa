package local

import (
	"testing"

	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve"
)

func TestBuildersPreserveFields(t *testing.T) {
	cache := packagejson.NewMemoryCache()
	input := &importmap.ImportMap{Imports: map[string]string{"x": "/x.js"}}
	workspaces := []resolve.WorkspacePackage{{Name: "a", Path: "/a"}}

	r := New(nil, nil).
		WithPackages([]string{"pkg"}).
		WithExclude([]string{"excluded"}).
		WithConditions([]string{"browser"}).
		WithInputMap(input).
		WithPathBase("/base").
		WithPackageDeps("/deps").
		WithIncludeRootExports().
		WithWorkspacePackages(workspaces).
		WithPackageCache(cache)

	tmplResolver, err := r.WithTemplate("/assets/{package}/{path}")
	if err != nil {
		t.Fatalf("WithTemplate failed: %v", err)
	}
	r = tmplResolver

	if len(r.additionalPackages) != 1 || r.additionalPackages[0] != "pkg" {
		t.Errorf("additionalPackages lost: %v", r.additionalPackages)
	}
	if len(r.excludePackages) != 1 || r.excludePackages[0] != "excluded" {
		t.Errorf("excludePackages lost: %v", r.excludePackages)
	}
	if len(r.conditions) != 1 || r.conditions[0] != "browser" {
		t.Errorf("conditions lost: %v", r.conditions)
	}
	if r.inputMap != input {
		t.Error("inputMap lost")
	}
	if r.pathBase != "/base" {
		t.Errorf("pathBase lost: %q", r.pathBase)
	}
	if r.packageDeps != "/deps" {
		t.Errorf("packageDeps lost: %q", r.packageDeps)
	}
	if !r.includeRootExports {
		t.Error("includeRootExports lost")
	}
	if len(r.workspacePackages) != 1 || r.workspacePackages[0].Name != "a" {
		t.Errorf("workspacePackages lost: %v", r.workspacePackages)
	}
	if r.cache != cache {
		t.Error("cache lost")
	}
	if r.template == nil {
		t.Error("template lost")
	}
}
