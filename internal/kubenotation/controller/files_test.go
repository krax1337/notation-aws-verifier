package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncTrustPolicyFilesRemovesStaleAndKeepsUnrelated(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Written by a TrustPolicy that was deleted (and finalized) by another replica.
	if err := os.WriteFile(filepath.Join(dir, "stale.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	desired := map[string][]byte{"aws.json": []byte(`{"version":"1.0"}`)}
	changed, err := syncTrustPolicyFiles(dir, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first sync reported no change")
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.json")); !os.IsNotExist(err) {
		t.Fatalf("stale trust policy not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins")); err != nil {
		t.Fatalf("unrelated directory removed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "aws.json"))
	if err != nil || string(got) != `{"version":"1.0"}` {
		t.Fatalf("aws.json = %q, %v", got, err)
	}

	changed, err = syncTrustPolicyFiles(dir, desired)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("identical sync reported a change")
	}
}

func TestSyncTrustStoresRemovesStale(t *testing.T) {
	root := filepath.Join(t.TempDir(), "truststore", "x509")
	stale := filepath.Join(root, "ca", "old")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, trustStoreCertFile), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	desired := map[string][]byte{filepath.Join("signingAuthority", "aws-signer-ts"): []byte("pem")}
	changed, err := syncTrustStores(root, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first sync reported no change")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale trust store not removed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "signingAuthority", "aws-signer-ts", trustStoreCertFile))
	if err != nil || string(got) != "pem" {
		t.Fatalf("certificates.crt = %q, %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "signingAuthority", "aws-signer-ts"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("trust store dir has %d entries (%v), want only %s", len(entries), err, trustStoreCertFile)
	}

	changed, err = syncTrustStores(root, desired)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("identical sync reported a change")
	}
}

func TestValidateFileName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../etc", "a/b", `a\b`} {
		if validateFileName(name) == nil {
			t.Errorf("validateFileName(%q) accepted an unsafe name", name)
		}
	}
	if err := validateFileName("aws-signer-tp"); err != nil {
		t.Errorf("validateFileName rejected a valid name: %v", err)
	}
}
