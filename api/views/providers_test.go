package views

import (
	"strings"
	"testing"
)

func TestProviderDashboardAssetsAreEmbedded(t *testing.T) {
	templateTree := Templates.Lookup("providers.html")
	if templateTree == nil {
		t.Fatal("providers template is not embedded")
	}
	templateSource := templateTree.Tree.Root.String()
	for _, marker := range []string{"models-dialog", "models-select-all", "models-import", "chat-dialog", "chat-send", "chat-stop"} {
		if !strings.Contains(templateSource, marker) {
			t.Errorf("providers template missing %q", marker)
		}
	}

	script, err := ReadAsset("app.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(script)
	for _, endpoint := range []string{
		"/test`",
		"/models`",
		"/models/import`",
		"/chat-tests`",
	} {
		if !strings.Contains(source, endpoint) {
			t.Errorf("provider dashboard script missing endpoint suffix %q", endpoint)
		}
	}
	for _, behavior := range []string{"TextDecoder", "AbortController", "Select all", "aria-busy", "selectedIndex"} {
		if !strings.Contains(source, behavior) {
			t.Errorf("provider dashboard script missing %q behavior", behavior)
		}
	}
}
