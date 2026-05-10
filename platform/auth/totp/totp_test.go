package totp

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestGenerateSecret(t *testing.T) {
	key, err := GenerateSecret("user@example.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if key.Issuer() != Issuer {
		t.Errorf("issuer: %q", key.Issuer())
	}
	if key.AccountName() != "user@example.com" {
		t.Errorf("account: %q", key.AccountName())
	}
	if key.Secret() == "" {
		t.Error("expected non-empty secret")
	}
}

func TestValidate(t *testing.T) {
	key, err := GenerateSecret("test@example.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if !Validate(code, key.Secret()) {
		t.Error("expected valid code to pass")
	}
}

func TestValidate_RejectsBadCode(t *testing.T) {
	key, _ := GenerateSecret("a@b")
	if Validate("000000", key.Secret()) {
		t.Error("expected wrong code to be rejected")
	}
}

func TestGenerateProvisioningURI(t *testing.T) {
	key, _ := GenerateSecret("test@example.com")
	uri := GenerateProvisioningURI(key.Secret(), "test@example.com")
	if !strings.HasPrefix(uri, "otpauth://") {
		t.Errorf("uri: %s", uri)
	}
}

func TestGenerateBackupCodes(t *testing.T) {
	plain, hashed, err := GenerateBackupCodes(DefaultBackupCodeCount)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(plain) != DefaultBackupCodeCount || len(hashed) != DefaultBackupCodeCount {
		t.Fatalf("expected %d codes, got %d/%d", DefaultBackupCodeCount, len(plain), len(hashed))
	}
	for i, c := range plain {
		if len(c) != BackupCodeLength {
			t.Errorf("code %d wrong length: %q", i, c)
		}
	}
	seen := make(map[string]bool)
	for _, c := range plain {
		if seen[c] {
			t.Errorf("duplicate backup code: %q", c)
		}
		seen[c] = true
	}
	if VerifyBackupCode(plain[0], hashed) != 0 {
		t.Error("expected first plain code to verify against first hash")
	}
}

func TestGenerateBackupCodes_DefaultsOnNonPositive(t *testing.T) {
	plain, _, err := GenerateBackupCodes(0)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(plain) != DefaultBackupCodeCount {
		t.Errorf("expected default count, got %d", len(plain))
	}
}

func TestVerifyBackupCode_Mismatch(t *testing.T) {
	_, hashed, _ := GenerateBackupCodes(3)
	if VerifyBackupCode("wrong", hashed) != -1 {
		t.Error("expected -1 for wrong code")
	}
}
