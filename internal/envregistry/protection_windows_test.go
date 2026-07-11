//go:build windows

package envregistry

import "testing"

func TestWindowsDPAPIRoundTrip(t *testing.T) {
	original := valuesFile{
		Env:     map[string]string{"PUBLIC": "value"},
		Secrets: map[string]string{"TOKEN": "中文-secret-value"},
	}
	protected, err := protectValuesForStorage(original)
	if err != nil {
		t.Fatal(err)
	}
	if protected.Protection != windowsDPAPIProtection {
		t.Fatalf("protection = %q", protected.Protection)
	}
	if protected.Secrets["TOKEN"] == original.Secrets["TOKEN"] {
		t.Fatal("secret remained plaintext")
	}
	plain, err := unprotectValuesFromStorage(protected)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Secrets["TOKEN"] != original.Secrets["TOKEN"] {
		t.Fatalf("secret = %q", plain.Secrets["TOKEN"])
	}
}
