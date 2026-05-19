package piv_test

// Test list:
//   - TestLoadDoDTrustStore_HappyPath — load testdata/dod_truststore_test.pem;
//     pool is non-nil and validates a leaf signed by one of the roots in
//     the bundle (full Verify roundtrip).
//   - TestLoadDoDTrustStore_EmptyFile — write empty file to t.TempDir();
//     assert ErrNoCertsInBundle.
//   - TestLoadDoDTrustStore_GarbageFile — write "not a pem cert\n" to
//     t.TempDir(); assert ErrNoCertsInBundle.
//   - TestLoadDoDTrustStore_NonexistentPath — load /tmp/nonexistent-X.pem;
//     assert errors.Is(err, os.ErrNotExist).

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/mtls/piv"
)

func TestLoadDoDTrustStore_HappyPath(t *testing.T) {
	bundlePath := filepath.Join("testdata", "dod_truststore_test.pem")
	pool, err := piv.LoadDoDTrustStore(bundlePath)
	if err != nil {
		t.Fatalf("LoadDoDTrustStore err: %v", err)
	}
	if pool == nil {
		t.Fatal("LoadDoDTrustStore returned nil pool with no error")
	}

	// Validate the pool actually works: a leaf in the bundle should
	// chain to itself when supplied as both leaf and root. We exercise
	// this by parsing sample_cac_root.pem (which is included in the
	// bundle) and verifying it chains against the loaded pool.
	rootPEM, err := os.ReadFile(filepath.Join("testdata", "sample_cac_root.pem"))
	if err != nil {
		t.Fatalf("read sample_cac_root.pem: %v", err)
	}
	block, _ := pem.Decode(rootPEM)
	if block == nil {
		t.Fatal("no PEM block in sample_cac_root.pem")
	}
	root, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse sample_cac_root.pem: %v", err)
	}
	// Verify against the loaded pool. The leaf == self-signed root,
	// which both lives in the pool, so chain-build must succeed.
	if _, err := root.Verify(x509.VerifyOptions{Roots: pool}); err != nil {
		t.Errorf("pool does not validate root from bundle: %v", err)
	}
}

func TestLoadDoDTrustStore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(emptyPath, []byte{}, 0o600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	pool, err := piv.LoadDoDTrustStore(emptyPath)
	if !errors.Is(err, piv.ErrNoCertsInBundle) {
		t.Fatalf("LoadDoDTrustStore(empty) err = %v, want ErrNoCertsInBundle", err)
	}
	if pool != nil {
		t.Errorf("LoadDoDTrustStore(empty) pool = %v, want nil", pool)
	}
}

func TestLoadDoDTrustStore_GarbageFile(t *testing.T) {
	dir := t.TempDir()
	garbagePath := filepath.Join(dir, "garbage.pem")
	if err := os.WriteFile(garbagePath, []byte("not a pem cert\n"), 0o600); err != nil {
		t.Fatalf("write garbage file: %v", err)
	}
	pool, err := piv.LoadDoDTrustStore(garbagePath)
	if !errors.Is(err, piv.ErrNoCertsInBundle) {
		t.Fatalf("LoadDoDTrustStore(garbage) err = %v, want ErrNoCertsInBundle", err)
	}
	if pool != nil {
		t.Errorf("LoadDoDTrustStore(garbage) pool = %v, want nil", pool)
	}
}

func TestLoadDoDTrustStore_NonexistentPath(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "does-not-exist.pem")
	pool, err := piv.LoadDoDTrustStore(missingPath)
	if err == nil {
		t.Fatalf("LoadDoDTrustStore(missing) returned nil err; pool=%v", pool)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("LoadDoDTrustStore(missing) err = %v, want errors.Is os.ErrNotExist", err)
	}
	if pool != nil {
		t.Errorf("LoadDoDTrustStore(missing) pool = %v, want nil", pool)
	}
}
