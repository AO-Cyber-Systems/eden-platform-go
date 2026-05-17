package signature

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestECDSAJWSFromDER(t *testing.T) {
	t.Run("p256_round_trip", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		digest := sha256.Sum256([]byte("hello"))
		derSig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
		require.NoError(t, err)

		raw, err := ECDSAJWSFromDER(derSig, 32)
		require.NoError(t, err)
		require.Equal(t, 64, len(raw), "raw signature for P-256 must be exactly 64 bytes")

		// Round-trip back to DER and verify the original signature still
		// validates (asn1 round-trip is bytewise canonical for ECDSA sigs
		// when integers are positive).
		der2, err := ECDSADERFromJWS(raw)
		require.NoError(t, err)
		require.True(t, ecdsa.VerifyASN1(&priv.PublicKey, digest[:], der2),
			"DER reconstructed from raw must verify against the original key")
	})

	t.Run("p384_round_trip", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		require.NoError(t, err)
		// We only need the digest size for the curve; use SHA-256 here for
		// simplicity — the conversion path is digest-agnostic.
		digest := sha256.Sum256([]byte("hello-p384"))
		derSig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
		require.NoError(t, err)

		raw, err := ECDSAJWSFromDER(derSig, 48)
		require.NoError(t, err)
		require.Equal(t, 96, len(raw), "raw signature for P-384 must be exactly 96 bytes")

		der2, err := ECDSADERFromJWS(raw)
		require.NoError(t, err)
		require.True(t, ecdsa.VerifyASN1(&priv.PublicKey, digest[:], der2))
	})

	t.Run("rejects_short_input", func(t *testing.T) {
		_, err := ECDSAJWSFromDER([]byte{0x30}, 32)
		require.Error(t, err)
	})

	t.Run("rejects_empty_input", func(t *testing.T) {
		_, err := ECDSAJWSFromDER(nil, 32)
		require.Error(t, err)
	})

	t.Run("rejects_invalid_curve_bytes", func(t *testing.T) {
		_, err := ECDSAJWSFromDER([]byte{0x30, 0x06, 0x02, 0x01, 0x01, 0x02, 0x01, 0x02}, 0)
		require.Error(t, err)
	})

	t.Run("left_pads_small_R", func(t *testing.T) {
		// Synthesize a DER signature where R = 1 (1 byte) and S = 1 (1 byte).
		// After conversion to raw with curveBytes=32, both halves must be
		// left-padded so the output is exactly 64 bytes.
		der, err := asn1.Marshal(ecdsaSig{R: big.NewInt(1), S: big.NewInt(1)})
		require.NoError(t, err)
		raw, err := ECDSAJWSFromDER(der, 32)
		require.NoError(t, err)
		require.Equal(t, 64, len(raw))
		// First 31 bytes of R half must be zero; byte 31 (the last R byte)
		// must be 0x01. Same for S half.
		for i := 0; i < 31; i++ {
			require.Equal(t, byte(0), raw[i], "R left-pad byte %d should be zero", i)
		}
		require.Equal(t, byte(1), raw[31])
		for i := 32; i < 63; i++ {
			require.Equal(t, byte(0), raw[i], "S left-pad byte %d should be zero", i)
		}
		require.Equal(t, byte(1), raw[63])
	})

	t.Run("rejects_R_exceeding_curve_width", func(t *testing.T) {
		// R has 33 bytes (too wide for P-256's 32-byte curveBytes).
		bigR := new(big.Int).Lsh(big.NewInt(1), 8*33)
		der, err := asn1.Marshal(ecdsaSig{R: bigR, S: big.NewInt(1)})
		require.NoError(t, err)
		_, err = ECDSAJWSFromDER(der, 32)
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds curve width")
	})

	t.Run("rejects_trailing_bytes", func(t *testing.T) {
		der, err := asn1.Marshal(ecdsaSig{R: big.NewInt(1), S: big.NewInt(1)})
		require.NoError(t, err)
		corrupted := append(der, 0xff, 0xff)
		_, err = ECDSAJWSFromDER(corrupted, 32)
		require.Error(t, err)
		require.Contains(t, err.Error(), "trailing bytes")
	})
}

func TestECDSADERFromJWS(t *testing.T) {
	t.Run("rejects_odd_length", func(t *testing.T) {
		_, err := ECDSADERFromJWS(make([]byte, 31))
		require.Error(t, err)
		require.Contains(t, err.Error(), "odd")
	})

	t.Run("rejects_empty", func(t *testing.T) {
		_, err := ECDSADERFromJWS(nil)
		require.Error(t, err)
	})

	t.Run("accepts_p256_width", func(t *testing.T) {
		raw := make([]byte, 64)
		raw[31] = 1
		raw[63] = 1
		der, err := ECDSADERFromJWS(raw)
		require.NoError(t, err)
		require.NotEmpty(t, der)
	})
}
