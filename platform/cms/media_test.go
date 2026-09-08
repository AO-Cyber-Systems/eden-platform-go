package cms

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/aocybersystems/eden-platform-go/platform/storage"
	"github.com/aocybersystems/eden-platform-go/platform/upload"
)

// encodePNG returns w x h PNG-encoded bytes of a solid-color test image.
func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encodePNG: %v", err)
	}
	return buf.Bytes()
}

// encodeJPEG returns w x h JPEG-encoded bytes of a solid-color test image.
func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: uint8(x % 256), B: uint8(y % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encodeJPEG: %v", err)
	}
	return buf.Bytes()
}

// tamperPNGDimensions returns a copy of a valid, CRC-correct PNG whose
// IHDR chunk declares width x height instead of whatever the source
// image actually was -- a spoofed header a naive decoder would allocate
// against before ever looking at the (unchanged, small) IDAT payload.
// The IHDR chunk's CRC32 is recomputed so the tampered header still
// parses as a syntactically valid PNG; only the declared dimensions lie.
func tamperPNGDimensions(t *testing.T, src []byte, width, height uint32) []byte {
	t.Helper()
	out := append([]byte(nil), src...)
	// PNG signature (8 bytes) + chunk length (4) + chunk type "IHDR" (4)
	// = 16-byte offset to the IHDR data field. Data field layout:
	// width(4) height(4) bitdepth(1) colortype(1) compression(1)
	// filter(1) interlace(1) = 13 bytes, offsets [16:33).
	if len(out) < 33 {
		t.Fatalf("tamperPNGDimensions: source too short to be a PNG with IHDR")
	}
	binary.BigEndian.PutUint32(out[16:20], width)
	binary.BigEndian.PutUint32(out[20:24], height)
	// CRC32 (IEEE) covers the chunk type + data: bytes [12:29).
	crc := crc32.ChecksumIEEE(out[12:29])
	binary.BigEndian.PutUint32(out[29:33], crc)
	return out
}

func newTestPipeline(cfg MediaPipelineConfig) *MediaPipeline {
	if cfg.Storage == nil {
		cfg.Storage = storage.NewMemoryClient("test", storage.Policy{})
	}
	return NewMediaPipeline(cfg)
}

func TestDeriveVariants_HappyPath_DerivesAllConfiguredVariants(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	companyID := uuid.New()
	data := encodePNG(t, 2000, 1000)

	media, err := p.DeriveVariants(context.Background(), companyID, UploadInput{
		Data: data, ContentType: "image/png", Filename: "source.png",
	})
	if err != nil {
		t.Fatalf("DeriveVariants: %v", err)
	}
	if media.CompanyID != companyID {
		t.Fatalf("CompanyID = %v, want %v", media.CompanyID, companyID)
	}
	if media.ContentHash == "" {
		t.Fatal("ContentHash is empty")
	}
	if len(media.Variants) != len(DefaultVariants) {
		t.Fatalf("got %d variants, want %d", len(media.Variants), len(DefaultVariants))
	}

	byName := make(map[string]VariantResult, len(media.Variants))
	for _, v := range media.Variants {
		byName[v.Name] = v
		if v.StorageKey == "" {
			t.Errorf("variant %q: empty StorageKey", v.Name)
		}
		if v.URL == "" {
			t.Errorf("variant %q: empty URL", v.Name)
		}
	}

	orig, ok := byName["original"]
	if !ok {
		t.Fatal("missing \"original\" variant")
	}
	if orig.Width != 2000 || orig.Height != 1000 {
		t.Fatalf("original dims = %dx%d, want 2000x1000 (passthrough, no resize)", orig.Width, orig.Height)
	}
	if orig.ContentType != "image/png" {
		t.Fatalf("original ContentType = %q, want image/png (unchanged passthrough)", orig.ContentType)
	}

	thumb, ok := byName["thumb"]
	if !ok {
		t.Fatal("missing \"thumb\" variant")
	}
	if thumb.Width != 320 {
		t.Fatalf("thumb Width = %d, want 320 (resized down from source)", thumb.Width)
	}
	if thumb.Height != 160 { // 320/2000 * 1000 = 160, aspect ratio preserved
		t.Fatalf("thumb Height = %d, want 160 (aspect-ratio preserved)", thumb.Height)
	}
}

func TestDeriveVariants_NeverUpscales(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	companyID := uuid.New()
	// Source is smaller than the "display" (1600px) ceiling.
	data := encodePNG(t, 100, 50)

	media, err := p.DeriveVariants(context.Background(), companyID, UploadInput{
		Data: data, ContentType: "image/png",
	})
	if err != nil {
		t.Fatalf("DeriveVariants: %v", err)
	}
	for _, v := range media.Variants {
		if v.Width > 100 {
			t.Errorf("variant %q Width = %d, upscaled beyond source width 100", v.Name, v.Width)
		}
	}
}

func TestDeriveVariants_Idempotent_SameSourceYieldsSameKeys(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	companyID := uuid.New()
	data := encodePNG(t, 2000, 1000)
	in := UploadInput{Data: data, ContentType: "image/png", Filename: "source.png"}

	ctx := context.Background()
	first, err := p.DeriveVariants(ctx, companyID, in)
	if err != nil {
		t.Fatalf("first DeriveVariants: %v", err)
	}
	second, err := p.DeriveVariants(ctx, companyID, in)
	if err != nil {
		t.Fatalf("second DeriveVariants: %v", err)
	}

	if first.ContentHash != second.ContentHash {
		t.Fatalf("ContentHash changed across calls: %q vs %q", first.ContentHash, second.ContentHash)
	}
	if len(first.Variants) != len(second.Variants) {
		t.Fatalf("variant count changed: %d vs %d", len(first.Variants), len(second.Variants))
	}
	for i := range first.Variants {
		a, b := first.Variants[i], second.Variants[i]
		if a.Name != b.Name {
			t.Fatalf("variant[%d] name changed: %q vs %q", i, a.Name, b.Name)
		}
		if a.StorageKey != b.StorageKey {
			t.Fatalf("variant %q StorageKey changed across calls: %q vs %q -- derivation is not idempotent", a.Name, a.StorageKey, b.StorageKey)
		}
		if a.Width != b.Width || a.Height != b.Height {
			t.Fatalf("variant %q dims changed across calls: %dx%d vs %dx%d", a.Name, a.Width, a.Height, b.Width, b.Height)
		}
	}
}

func TestDeriveVariants_DifferentTenantsGetDifferentKeys(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	data := encodePNG(t, 400, 300)
	in := UploadInput{Data: data, ContentType: "image/png"}

	ctx := context.Background()
	a, err := p.DeriveVariants(ctx, uuid.New(), in)
	if err != nil {
		t.Fatalf("DeriveVariants (tenant A): %v", err)
	}
	b, err := p.DeriveVariants(ctx, uuid.New(), in)
	if err != nil {
		t.Fatalf("DeriveVariants (tenant B): %v", err)
	}

	if a.ContentHash != b.ContentHash {
		t.Fatalf("ContentHash should be identical across tenants for identical bytes: %q vs %q", a.ContentHash, b.ContentHash)
	}
	for i := range a.Variants {
		if a.Variants[i].StorageKey == b.Variants[i].StorageKey {
			t.Fatalf("variant %q StorageKey collided across tenants: %q -- tenant isolation broken", a.Variants[i].Name, a.Variants[i].StorageKey)
		}
	}
}

func TestDeriveVariants_JPEGSource(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	data := encodeJPEG(t, 1000, 500)

	media, err := p.DeriveVariants(context.Background(), uuid.New(), UploadInput{
		Data: data, ContentType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("DeriveVariants: %v", err)
	}
	if len(media.Variants) != len(DefaultVariants) {
		t.Fatalf("got %d variants, want %d", len(media.Variants), len(DefaultVariants))
	}
}

func TestDeriveVariants_EmptyData(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	_, err := p.DeriveVariants(context.Background(), uuid.New(), UploadInput{
		Data: nil, ContentType: "image/png",
	})
	if !errors.Is(err, ErrMediaEmpty) {
		t.Fatalf("err = %v, want ErrMediaEmpty", err)
	}
}

func TestDeriveVariants_TooLarge_RejectedBeforeDecode(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{MaxBytes: 10})
	// Deliberately not a valid image -- proves the size check runs and
	// rejects BEFORE any decode attempt would even be reached.
	_, err := p.DeriveVariants(context.Background(), uuid.New(), UploadInput{
		Data: []byte("not-an-image-and-way-too-long-for-the-10-byte-cap"), ContentType: "image/png",
	})
	if !errors.Is(err, ErrMediaTooLarge) {
		t.Fatalf("err = %v, want ErrMediaTooLarge", err)
	}
}

func TestDeriveVariants_UnsupportedContentType(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	_, err := p.DeriveVariants(context.Background(), uuid.New(), UploadInput{
		Data: []byte("<svg></svg>"), ContentType: "image/svg+xml",
	})
	if !errors.Is(err, ErrMediaUnsupportedType) {
		t.Fatalf("err = %v, want ErrMediaUnsupportedType", err)
	}
}

// TestDeriveVariants_MalformedImage_TypedErrorNeverPanic proves that
// bytes claiming an allowed content type but containing garbage (not any
// valid image encoding) are rejected with ErrMediaMalformed, and that
// DeriveVariants does not panic while doing so.
func TestDeriveVariants_MalformedImage_TypedErrorNeverPanic(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	garbage := bytes.Repeat([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 64)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DeriveVariants panicked on malformed image: %v", r)
		}
	}()
	_, err := p.DeriveVariants(context.Background(), uuid.New(), UploadInput{
		Data: garbage, ContentType: "image/png",
	})
	if !errors.Is(err, ErrMediaMalformed) {
		t.Fatalf("err = %v, want ErrMediaMalformed", err)
	}
}

// TestDeriveVariants_SpoofedDimensions_RejectedByDecodeConfigBoundsCheck
// proves the specific mechanism this TRD requires: a syntactically valid,
// CRC-correct image whose HEADER declares dimensions beyond
// MaxMediaDimension is rejected using image.DecodeConfig's reported
// Width/Height, BEFORE the memory-allocating image.Decode call runs --
// never a panic, and never a full-decode attempt against the spoofed size.
func TestDeriveVariants_SpoofedDimensions_RejectedByDecodeConfigBoundsCheck(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	base := encodePNG(t, 1, 1)
	spoofed := tamperPNGDimensions(t, base, 60000, 60000)

	// Confirm the tamper produced a header image.DecodeConfig can still
	// parse (CRC-valid) and that it reports the spoofed size -- otherwise
	// this test would be exercising a different code path than intended.
	cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(spoofed))
	if cfgErr != nil {
		t.Fatalf("tampered PNG failed to parse at all (bad test fixture): %v", cfgErr)
	}
	if cfg.Width != 60000 || cfg.Height != 60000 {
		t.Fatalf("tampered PNG reports %dx%d, want 60000x60000 (bad test fixture)", cfg.Width, cfg.Height)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DeriveVariants panicked on spoofed-dimension image: %v", r)
		}
	}()
	_, err := p.DeriveVariants(context.Background(), uuid.New(), UploadInput{
		Data: spoofed, ContentType: "image/png",
	})
	if !errors.Is(err, ErrMediaMalformed) {
		t.Fatalf("err = %v, want ErrMediaMalformed", err)
	}
	if !strings.Contains(err.Error(), "60000") {
		t.Fatalf("err = %v, want it to reference the rejected dimension", err)
	}
}

func TestDeriveVariants_NilCompanyID(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	_, err := p.DeriveVariants(context.Background(), uuid.Nil, UploadInput{
		Data: encodePNG(t, 10, 10), ContentType: "image/png",
	})
	if err == nil {
		t.Fatal("want error for uuid.Nil companyID, got nil")
	}
}

func TestDeriveVariants_StorageDisabled(t *testing.T) {
	p := NewMediaPipeline(MediaPipelineConfig{}) // Storage left nil
	_, err := p.DeriveVariants(context.Background(), uuid.New(), UploadInput{
		Data: encodePNG(t, 10, 10), ContentType: "image/png",
	})
	if !errors.Is(err, ErrMediaStorageDisabled) {
		t.Fatalf("err = %v, want ErrMediaStorageDisabled", err)
	}
}

func TestMediaPipeline_PresignVariant_StorageDisabled(t *testing.T) {
	p := NewMediaPipeline(MediaPipelineConfig{})
	_, err := p.PresignVariant(context.Background(), "cms-media/x/y/original", 0)
	if !errors.Is(err, ErrMediaStorageDisabled) {
		t.Fatalf("err = %v, want ErrMediaStorageDisabled", err)
	}
}

func TestMediaPipeline_PresignVariant_ReturnsURL(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	companyID := uuid.New()
	media, err := p.DeriveVariants(context.Background(), companyID, UploadInput{
		Data: encodePNG(t, 50, 50), ContentType: "image/png",
	})
	if err != nil {
		t.Fatalf("DeriveVariants: %v", err)
	}
	key := media.Variants[0].StorageKey
	url, err := p.PresignVariant(context.Background(), key, 0)
	if err != nil {
		t.Fatalf("PresignVariant: %v", err)
	}
	if url == "" {
		t.Fatal("PresignVariant returned empty URL")
	}
}

// TestDeriveFromAttachment_ComposesPlatformUpload proves MediaPipeline
// composes platform/upload's Attachment shape: a caller that already
// completed a presigned-URL upload through platform/upload hands over
// the resulting Attachment (StorageKey/ContentType/Filename only -- no
// DB or minio.Client required) and DeriveFromAttachment fetches those
// bytes through the SAME storage.Client and derives identical variants
// to the direct DeriveVariants path.
func TestDeriveFromAttachment_ComposesPlatformUpload(t *testing.T) {
	store := storage.NewMemoryClient("test", storage.Policy{})
	p := newTestPipeline(MediaPipelineConfig{Storage: store})
	ctx := context.Background()
	companyID := uuid.New()
	data := encodePNG(t, 640, 480)

	// Simulate what platform/upload.Service.CompleteUpload already did:
	// the source bytes are sitting in the bucket under an
	// upload-service-chosen key, described by an Attachment.
	const uploadKey = "uploads/company/attachment-id/source.png"
	if _, err := store.Put(ctx, uploadKey, bytes.NewReader(data), "image/png", int64(len(data)), nil); err != nil {
		t.Fatalf("seed upload object: %v", err)
	}
	attachment := upload.Attachment{
		StorageKey:  uploadKey,
		ContentType: "image/png",
		Filename:    "source.png",
		SizeBytes:   int64(len(data)),
	}

	viaAttachment, err := p.DeriveFromAttachment(ctx, companyID, attachment)
	if err != nil {
		t.Fatalf("DeriveFromAttachment: %v", err)
	}
	viaDirect, err := p.DeriveVariants(ctx, companyID, UploadInput{
		Data: data, ContentType: "image/png", Filename: "source.png",
	})
	if err != nil {
		t.Fatalf("DeriveVariants: %v", err)
	}

	if viaAttachment.ContentHash != viaDirect.ContentHash {
		t.Fatalf("ContentHash mismatch: attachment path %q vs direct path %q", viaAttachment.ContentHash, viaDirect.ContentHash)
	}
	for i := range viaAttachment.Variants {
		if viaAttachment.Variants[i].StorageKey != viaDirect.Variants[i].StorageKey {
			t.Fatalf("variant %q StorageKey mismatch: attachment path %q vs direct path %q",
				viaAttachment.Variants[i].Name, viaAttachment.Variants[i].StorageKey, viaDirect.Variants[i].StorageKey)
		}
	}
}

func TestDeriveFromAttachment_StorageDisabled(t *testing.T) {
	p := NewMediaPipeline(MediaPipelineConfig{})
	_, err := p.DeriveFromAttachment(context.Background(), uuid.New(), upload.Attachment{StorageKey: "x"})
	if !errors.Is(err, ErrMediaStorageDisabled) {
		t.Fatalf("err = %v, want ErrMediaStorageDisabled", err)
	}
}

func TestDeriveFromAttachment_MissingObject(t *testing.T) {
	p := newTestPipeline(MediaPipelineConfig{})
	_, err := p.DeriveFromAttachment(context.Background(), uuid.New(), upload.Attachment{
		StorageKey: "does/not/exist", ContentType: "image/png",
	})
	if err == nil {
		t.Fatal("want error for missing attachment object, got nil")
	}
}

func TestDefaultVariants_IsGenericSizingOnly(t *testing.T) {
	// Guards the TRD's block-catalog isolation requirement: the shipped
	// default variant catalog names renditions by SIZE only
	// ("original"/"thumb"/"display") and carries no page-builder or
	// donor-specific block vocabulary.
	allowed := map[string]bool{"original": true, "thumb": true, "display": true}
	for _, v := range DefaultVariants {
		if !allowed[v.Name] {
			t.Fatalf("DefaultVariants has an unexpected, non-generic variant name: %q", v.Name)
		}
	}
}
