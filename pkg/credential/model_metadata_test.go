package credential

import (
	"testing"
	"time"
)

func TestMetadataSnapshotNormalizesProviderCapabilities(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.FixedZone("test", 3600))
	metadata := MetadataSnapshot(" codex ", " cred-1 ", ProviderModel{
		Name: " Future ", ContextLength: 200000, MaxContextWindow: 800000,
		Capabilities: map[string]any{"vision": true, "effort_tiers": []any{"Low", "high", "low"}},
	}, now)
	if metadata.Provider != "codex" || metadata.SourceCredentialID != "cred-1" || metadata.DisplayName != "Future" || metadata.RefreshedAt.Location() != time.UTC {
		t.Fatalf("metadata = %+v", metadata)
	}
	if len(metadata.InputModalities) != 2 || metadata.InputModalities[1] != "image" || len(metadata.SupportedReasoningLevels) != 2 || metadata.SupportedReasoningLevels[1].Effort != "high" {
		t.Fatalf("capabilities = %+v", metadata)
	}
}

func TestMetadataSnapshotDoesNotInventUnknownCapabilities(t *testing.T) {
	metadata := MetadataSnapshot("custom", "cred", ProviderModel{}, time.Now())
	if len(metadata.InputModalities) != 1 || metadata.InputModalities[0] != "text" || len(metadata.SupportedReasoningLevels) != 0 || metadata.SupportsOriginalImage {
		t.Fatalf("metadata = %+v", metadata)
	}
}
