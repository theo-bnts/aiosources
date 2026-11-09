package model

import "testing"

func TestCatalogIdentityKeepsTheReleasedProtocol(t *testing.T) {
	origin := CatalogOrigin{InstanceID: "fixture-instance", ProviderID: "fixture-source", SourceID: "item-42"}
	const published = "b495859f20ae6aab5a0858a05ac1ea6f0cceed6bd53fd569f04fe3155c3ab267"
	if got := CatalogItemID(origin); got != published {
		t.Fatal("catalog identity changed; synchronized records would be duplicated")
	}
}
