package main

import "testing"

func TestInitializeMigratesOnlyOldDefaultLogo(t *testing.T) {
	a, _ := setup(t)
	settings, err := a.store.settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.LogoURL != "/mark.png" {
		t.Fatalf("new installations should use the PNG mark, got %q", settings.LogoURL)
	}

	settings.LogoURL = "/mark.svg"
	if err := a.store.saveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := initialize(a.store, true); err != nil {
		t.Fatal(err)
	}
	settings, err = a.store.settings()
	if err != nil || settings.LogoURL != "/mark.png" {
		t.Fatalf("old default mark was not migrated: logo=%q err=%v", settings.LogoURL, err)
	}

	settings.LogoURL = "/api/public/logo/custom-id"
	if err := a.store.saveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := initialize(a.store, true); err != nil {
		t.Fatal(err)
	}
	settings, err = a.store.settings()
	if err != nil || settings.LogoURL != "/api/public/logo/custom-id" {
		t.Fatalf("startup overwrote administrator logo: logo=%q err=%v", settings.LogoURL, err)
	}
}
