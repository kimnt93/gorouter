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
	for _, marker := range []string{"models-dialog", "models-select-all", "models-import", "chat-dialog", "chat-send", "chat-stop", "oauth-callback-field", "oauth-device-flow", "oauth-copy-code"} {
		if !strings.Contains(templateSource, marker) {
			t.Errorf("providers template missing %q", marker)
		}
	}
	providerCard := Templates.Lookup("providerCard")
	if providerCard == nil || !strings.Contains(providerCard.Tree.Root.String(), "provider-account-actions") {
		t.Error("provider card template missing responsive account action group")
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
	for _, behavior := range []string{"TextDecoder", "AbortController", "Select all", "aria-busy", "selectedIndex", "Check authorization", "navigator.clipboard"} {
		if !strings.Contains(source, behavior) {
			t.Errorf("provider dashboard script missing %q behavior", behavior)
		}
	}

	stylesheet, err := ReadAsset("app.css")
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesheet)
	for _, selector := range []string{".provider-account-actions", ".provider-grid>*", ".provider-card[open]{grid-column:1/-1", ".oauth-flow-panel[hidden]", "overflow-y:auto"} {
		if !strings.Contains(styles, selector) {
			t.Errorf("provider dashboard stylesheet missing responsive selector %q", selector)
		}
	}
}
