package missions

import _ "embed"

//go:embed missions.json
var defaultCatalogJSON []byte

// DefaultCatalog returns the embedded v0.6.5 starter catalog.
func DefaultCatalog() (Catalog, error) {
	return LoadCatalog(defaultCatalogJSON)
}
