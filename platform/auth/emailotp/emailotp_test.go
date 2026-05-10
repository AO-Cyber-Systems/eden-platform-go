package emailotp

import (
	"strconv"
	"testing"
	"time"
)

func TestGenerate(t *testing.T) {
	for i := 0; i < 50; i++ {
		code, err := Generate()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if len(code) != CodeLength {
			t.Errorf("expected %d digits, got %d (%q)", CodeLength, len(code), code)
		}
		if _, err := strconv.Atoi(code); err != nil {
			t.Errorf("code is not numeric: %q", code)
		}
	}
}

func TestVerify_Success(t *testing.T) {
	if !Verify("123456", "123456", time.Now(), DefaultExpiry) {
		t.Error("expected verify to succeed for matching code within window")
	}
}

func TestVerify_DefaultExpiryWhenZero(t *testing.T) {
	if !Verify("123456", "123456", time.Now(), 0) {
		t.Error("expected zero expiry to fall back to DefaultExpiry")
	}
}

func TestVerify_Expired(t *testing.T) {
	old := time.Now().Add(-(DefaultExpiry + time.Minute))
	if Verify("123456", "123456", old, DefaultExpiry) {
		t.Error("expected expired code to fail")
	}
}

func TestVerify_Mismatch(t *testing.T) {
	if Verify("123456", "999999", time.Now(), DefaultExpiry) {
		t.Error("expected mismatched code to fail")
	}
}

func TestVerify_Empty(t *testing.T) {
	if Verify("", "123456", time.Now(), DefaultExpiry) {
		t.Error("expected empty stored code to fail")
	}
	if Verify("123456", "", time.Now(), DefaultExpiry) {
		t.Error("expected empty provided code to fail")
	}
}
