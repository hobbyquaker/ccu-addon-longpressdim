// Package addonpkg tests the package files the addon ships.
package addonpkg

import (
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit/manifest"
)

// The manifest as the system validates it at install, through the kit's strict load (an unknown
// field fails), with the release notes the Addons page links beside an offered update.
func TestManifest(t *testing.T) {
	m, err := manifest.Load("../../addon/openccu-lite.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "longpressdim" || m.Undeclared() || m.Runtime == nil || !m.Runtime.Daemon {
		t.Fatalf("manifest: %+v", m)
	}
	if m.Changelog != "https://github.com/hobbyquaker/ccu-addon-longpressdim/blob/main/CHANGELOG.md" {
		t.Errorf("changelog: %q", m.Changelog)
	}
	if m.Description["de"] == "" || m.Description["en"] == "" {
		t.Error("the description in both languages")
	}
}

// The system's catalogue check reads the manifest's images from the repository at the release tag,
// beside the manifest (the manifest's directory is taken as the package root), and the install
// reads them from the package at the same paths. So every declared image must be a file the
// repository tracks at that path - not one the build copies in from elsewhere.
func TestManifestImagesAreInTheRepository(t *testing.T) {
	b, err := os.ReadFile("../../addon/openccu-lite.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		UI map[string]any `json:"ui"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, key := range []string{"icon", "icon_dark", "logo", "logo_dark"} {
		v, ok := m.UI[key]
		if !ok {
			continue
		}
		p, _ := v.(string)
		if p == "" || path.IsAbs(p) || strings.Contains(p, "..") {
			t.Errorf("ui.%s: %q is not a path relative to the package root", key, v)
			continue
		}
		n++
		rel := "addon/" + p
		out, err := exec.Command("git", "-C", "../..", "ls-files", "--error-unmatch", "--", rel).CombinedOutput()
		if err != nil {
			t.Errorf("ui.%s: %s is not tracked in the repository (the catalogue would answer 404): %v %s", key, rel, err, out)
			continue
		}
		img, err := os.ReadFile("../../" + rel)
		if err != nil {
			t.Errorf("ui.%s: %v", key, err)
			continue
		}
		if len(img) > 256<<10 || !strings.Contains(string(img[:min(len(img), 512)]), "<svg") {
			t.Errorf("ui.%s: %s is not an SVG of at most 256 KiB", key, rel)
		}
	}
	if n == 0 {
		t.Error("the manifest declares no image")
	}
}

// The page lighttpd answers while the service is down names the addon as the manifest does.
func TestErrorPageName(t *testing.T) {
	b, err := os.ReadFile("../../addon/openccu-lite.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile("../../addon/static/lighttpd-error-503.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<title>" + m.Name + "</title>", "<h1>" + m.Name + "</h1>"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("the 503 page lacks %q", want)
		}
	}
}
