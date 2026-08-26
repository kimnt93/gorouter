package spa

import (
	"regexp"
	"strings"
	"testing"
)

func TestBuiltApplicationAndAssetsAreEmbedded(t *testing.T) {
	index, err := Index()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `<div id="root"></div>`) {
		t.Fatal("built index is missing the React root")
	}
	assets := regexp.MustCompile(`/app-assets/assets/([^"']+)`).FindAllSubmatch(index, -1)
	if len(assets) < 2 {
		t.Fatalf("built index contains %d assets, want JavaScript and CSS", len(assets))
	}
	for _, match := range assets {
		if _, err := Asset("assets/" + string(match[1])); err != nil {
			t.Fatalf("embedded asset %q: %v", match[1], err)
		}
	}
}
