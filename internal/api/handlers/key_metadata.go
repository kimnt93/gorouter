package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func keyMetadataRevision(k *entities.ApiKey) string {
	scopes := append([]string{}, k.Scopes...)
	sort.Strings(scopes)
	raw, _ := json.Marshal(CanonicalKeyMetadataResponse{KeyID: k.ID, OwnerID: k.OwnerUserID, Enabled: k.Enabled, Scopes: scopes})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
