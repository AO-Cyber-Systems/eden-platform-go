package cms

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // register GIF decoder for image.DecodeConfig / image.Decode
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig / image.Decode
	"image/png"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/aocybersystems/eden-platform-go/platform/storage"
	"github.com/aocybersystems/eden-platform-go/platform/upload"
)

// This file is the image pipeline behind CMS content: ingest an upload,
// derive the size-bounded renditions a page needs, and hand back stable
// storage keys + presigned URLs. Video is explicitly OUT of scope for V1
// (donor repos ship a video_service.go / video_url.go alongside their
// image pipeline; this package derives none of that -- a consumer needing
// video renditions builds its own pipeline, or a later TRD extends this
// one deliberately rather than this file silently growing video support
// as a side effect).
//
// MediaPipeline never talks to an object-store SDK or a filesystem
// directly. Every byte in or out goes through the injected
// storage.Client -- that is the ONE blob path this package takes, per
// this TRD's constraint against a second blob implementation living
// inside cms.

// MaxMediaBytes caps a raw upload BEFORE any decode attempt. It is
// declared independently of platform/storage.DefaultMaxBytes since CMS
// media policy may legitimately differ from the generic object-store
// ceiling; MediaPipelineConfig.MaxBytes overrides it per pipeline.
const MaxMediaBytes int64 = 10 * 1024 * 1024 // 10 MB

// MaxMediaDimension bounds the width AND height image.DecodeConfig may
// report before DeriveVariants proceeds to the memory-allocating
// image.Decode call. A crafted image header can declare enormous
// dimensions while the byte stream itself is tiny (a "decompression
// bomb") -- checking DecodeConfig's reported Width/Height against this
// ceiling BEFORE calling image.Decode is what keeps a malformed upload
// from exhausting memory rather than merely failing to decode.
const MaxMediaDimension = 8000

// Errors returned by MediaPipeline. All are typed and wrapped with
// fmt.Errorf("%w: ...") for context -- DeriveVariants never panics on
// caller-supplied bytes; every rejection path returns one of these.
var (
	// ErrMediaEmpty is returned when the uploaded byte slice is empty.
	ErrMediaEmpty = errors.New("cms: media data is empty")

	// ErrMediaTooLarge is returned when the uploaded byte slice exceeds
	// the pipeline's configured MaxBytes, checked BEFORE any decode
	// attempt.
	ErrMediaTooLarge = errors.New("cms: media exceeds maximum size")

	// ErrMediaUnsupportedType is returned for a content type outside the
	// decodable whitelist (image/jpeg, image/png, image/gif).
	ErrMediaUnsupportedType = errors.New("cms: unsupported media content type")

	// ErrMediaMalformed is returned when the declared content type's
	// decoder cannot parse the bytes, or image.DecodeConfig's reported
	// dimensions exceed MaxMediaDimension. This is the single error a
	// caller sees for "not a valid, safely-decodable image" -- never a
	// panic.
	ErrMediaMalformed = errors.New("cms: media is malformed or exceeds safe decode bounds")

	// ErrMediaStorageDisabled is returned by every byte-moving method on
	// a MediaPipeline constructed with a nil storage.Client. A nil
	// Storage is accepted at construction -- so a caller can wire up the
	// rest of a server when the blob backend is down -- but every call
	// that would move bytes fails closed, matching this package's other
	// services' graceful-degradation convention.
	ErrMediaStorageDisabled = errors.New("cms: media storage disabled")
)

// mediaContentTypes is the closed whitelist of content types
// DeriveVariants will attempt to decode. Restricted to formats the
// standard library decodes without an additional module dependency
// (golang.org/x/image/... is out of this TRD's scope -- go.mod is not a
// file this TRD owns).
var mediaContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
}

// MediaVariant is one derived rendition of a source image: a name and the
// maximum width (in px) DeriveVariants downscales to. Height scales to
// preserve the source's aspect ratio. Width <= 0 means "no resize" -- the
// source bytes pass through unchanged under that name (conventionally
// "original").
//
// Name is caller-opaque, exactly like Block.Type -- this package never
// special-cases a variant name. DefaultVariants is a generic starting
// catalog with no consumer-specific vocabulary; a consumer with different
// rendition needs constructs its own []MediaVariant.
type MediaVariant struct {
	Name  string
	Width int
}

// DefaultVariants is a generic three-rendition catalog: the unmodified
// source, a small thumbnail, and a display-sized rendition. It carries no
// page-builder or block-catalog vocabulary -- callers needing a different
// set pass their own to NewMediaPipeline via MediaPipelineConfig.Variants.
var DefaultVariants = []MediaVariant{
	{Name: "original", Width: 0},
	{Name: "thumb", Width: 320},
	{Name: "display", Width: 1600},
}

// UploadInput is the raw material DeriveVariants accepts: bytes plus the
// caller's declared content type. Filename is optional bookkeeping only
// -- DeriveVariants never folds it into a storage key, which is what
// keeps derivation content-addressed and idempotent regardless of what a
// caller names the file.
type UploadInput struct {
	Data        []byte
	ContentType string
	Filename    string
}

// VariantResult is one derived rendition: its name, the storage key it
// was (or already was) written under, its actual pixel dimensions after
// any resize, its content type, and a presigned URL a page can embed
// directly.
//
// StorageKey is the STABLE identifier -- deterministic for a given
// (tenant, source bytes, variant name) triple, see MediaPipeline.
// URL is a presigned GET URL freshly minted on this call and therefore
// NOT expected to compare equal across separate DeriveVariants calls
// (platform/storage's presigned URLs embed an expiry); callers that need
// a fresh URL later call PresignVariant again with the same StorageKey.
type VariantResult struct {
	Name        string
	StorageKey  string
	Width       int
	Height      int
	ContentType string
	URL         string
}

// Media is the result of one DeriveVariants (or DeriveFromAttachment)
// call: the source's content hash -- the identity derivation is
// idempotent on -- and one VariantResult per configured MediaVariant.
type Media struct {
	CompanyID   uuid.UUID
	ContentHash string // hex sha256 of the exact source bytes
	Variants    []VariantResult
}

// MediaPipelineConfig configures a MediaPipeline.
type MediaPipelineConfig struct {
	// Storage is the blob backend every byte-moving method routes
	// through. A nil Storage is accepted (see ErrMediaStorageDisabled).
	Storage storage.Client

	// Variants is the catalog DeriveVariants derives on every call. Nil
	// or empty defaults to DefaultVariants.
	Variants []MediaVariant

	// MaxBytes caps a raw upload before any decode attempt. <= 0
	// defaults to MaxMediaBytes.
	MaxBytes int64

	// KeyPrefix namespaces every object key this pipeline writes. Empty
	// defaults to "cms-media".
	KeyPrefix string

	// URLTTL is how long a presigned GET URL minted by DeriveVariants /
	// PresignVariant stays valid. Zero defers to the storage.Client's own
	// default (see platform/storage.Policy.DefaultExpiry).
	URLTTL time.Duration
}

// MediaPipeline derives the size-bounded renditions a page needs from one
// uploaded source image, storing each rendition through platform/storage
// and handing back stable, presignable keys. It composes platform/storage
// (all blob IO) and platform/upload (DeriveFromAttachment accepts an
// upload.Attachment as its source) rather than reimplementing either.
type MediaPipeline struct {
	storage   storage.Client
	variants  []MediaVariant
	maxBytes  int64
	keyPrefix string
	urlTTL    time.Duration
}

// NewMediaPipeline constructs a MediaPipeline from cfg, applying defaults
// for any zero-valued field.
func NewMediaPipeline(cfg MediaPipelineConfig) *MediaPipeline {
	variants := cfg.Variants
	if len(variants) == 0 {
		variants = DefaultVariants
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxMediaBytes
	}
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "cms-media"
	}
	return &MediaPipeline{
		storage:   cfg.Storage,
		variants:  variants,
		maxBytes:  maxBytes,
		keyPrefix: prefix,
		urlTTL:    cfg.URLTTL,
	}
}

// DeriveVariants validates in, derives every configured MediaVariant, and
// stores each rendition through the pipeline's storage.Client.
//
// Validation and decode order is deliberate, matching this TRD's
// constraint that untrusted bytes are never decoded without bounds:
//
//  1. Size check (len(in.Data) only -- before any decode call)
//  2. Content-type whitelist
//  3. image.DecodeConfig -- reads the header only, no pixel buffer
//  4. Reported-dimension bounds check (MaxMediaDimension) -- BEFORE the
//     full, memory-allocating image.Decode call
//  5. image.Decode -- only reached once 1-4 pass, and only if at least
//     one configured variant actually needs resized pixels
//
// Derivation is idempotent and content-addressed: the storage key for
// each variant is a deterministic function of (companyID, sha256(data),
// variant name). Calling DeriveVariants twice with the same companyID and
// the same source bytes always yields the same Media.ContentHash and the
// same VariantResult.StorageKey per variant -- re-processing never
// creates a second copy, and a variant already present in storage is
// reused (its Put is skipped) rather than re-derived.
func (p *MediaPipeline) DeriveVariants(ctx context.Context, companyID uuid.UUID, in UploadInput) (Media, error) {
	if p.storage == nil {
		return Media{}, ErrMediaStorageDisabled
	}
	if companyID == uuid.Nil {
		return Media{}, errors.New("cms: media requires a non-nil company id")
	}
	if len(in.Data) == 0 {
		return Media{}, ErrMediaEmpty
	}
	if int64(len(in.Data)) > p.maxBytes {
		return Media{}, fmt.Errorf("%w: %d bytes exceeds %d max", ErrMediaTooLarge, len(in.Data), p.maxBytes)
	}
	if !mediaContentTypes[in.ContentType] {
		return Media{}, fmt.Errorf("%w: %q", ErrMediaUnsupportedType, in.ContentType)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(in.Data))
	if err != nil {
		return Media{}, fmt.Errorf("%w: decode config: %v", ErrMediaMalformed, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxMediaDimension || cfg.Height > MaxMediaDimension {
		return Media{}, fmt.Errorf("%w: %dx%d exceeds %d max dimension", ErrMediaMalformed, cfg.Width, cfg.Height, MaxMediaDimension)
	}

	sum := sha256.Sum256(in.Data)
	hashHex := hex.EncodeToString(sum[:])

	var decoded image.Image // lazily decoded only if a resize is actually needed
	results := make([]VariantResult, 0, len(p.variants))
	for _, v := range p.variants {
		key := p.variantKey(companyID, hashHex, v.Name)

		if existing, statErr := p.storage.Stat(ctx, key); statErr == nil {
			w, h := metaDims(existing.Metadata)
			url, presignErr := p.storage.PresignedGet(ctx, key, p.urlTTL)
			if presignErr != nil {
				return Media{}, fmt.Errorf("cms: presign existing variant %q: %w", v.Name, presignErr)
			}
			results = append(results, VariantResult{
				Name: v.Name, StorageKey: key, Width: w, Height: h,
				ContentType: existing.ContentType, URL: url,
			})
			continue
		} else if !errors.Is(statErr, storage.ErrNotFound) {
			return Media{}, fmt.Errorf("cms: stat variant %q: %w", v.Name, statErr)
		}

		var (
			body          []byte
			contentType   string
			width, height int
		)
		if v.Width <= 0 || cfg.Width <= v.Width {
			// Passthrough: the variant ceiling is "no resize", or the
			// source is already at or under the ceiling. Store the exact
			// uploaded bytes -- "original" (and any variant wider than
			// the source) is never a re-encode.
			body = in.Data
			contentType = in.ContentType
			width, height = cfg.Width, cfg.Height
		} else {
			if decoded == nil {
				decoded, _, err = image.Decode(bytes.NewReader(in.Data))
				if err != nil {
					return Media{}, fmt.Errorf("%w: decode: %v", ErrMediaMalformed, err)
				}
			}
			resized := resizeToWidth(decoded, v.Width)
			var buf bytes.Buffer
			if err := png.Encode(&buf, resized); err != nil {
				return Media{}, fmt.Errorf("cms: encode variant %q: %w", v.Name, err)
			}
			body = buf.Bytes()
			contentType = "image/png"
			b := resized.Bounds()
			width, height = b.Dx(), b.Dy()
		}

		if _, err := p.storage.Put(ctx, key, bytes.NewReader(body), contentType, int64(len(body)), map[string]string{
			"width":  strconv.Itoa(width),
			"height": strconv.Itoa(height),
		}); err != nil {
			return Media{}, fmt.Errorf("cms: put variant %q: %w", v.Name, err)
		}

		url, err := p.storage.PresignedGet(ctx, key, p.urlTTL)
		if err != nil {
			return Media{}, fmt.Errorf("cms: presign variant %q: %w", v.Name, err)
		}

		results = append(results, VariantResult{
			Name: v.Name, StorageKey: key, Width: width, Height: height,
			ContentType: contentType, URL: url,
		})
	}

	return Media{CompanyID: companyID, ContentHash: hashHex, Variants: results}, nil
}

// DeriveFromAttachment derives variants for a source that was uploaded
// through platform/upload's presigned-URL flow rather than handed to this
// package directly as raw bytes: it fetches the object platform/upload
// already wrote -- keyed by a.StorageKey, the field
// upload.Service.CompleteUpload's Attachment carries -- via
// storage.Client.Get, then runs the identical DeriveVariants path.
//
// This assumes a's bytes are reachable through this pipeline's own
// storage.Client (i.e. platform/upload and this MediaPipeline are wired
// to the same bucket/backend). A deployment that keeps upload attachments
// in a separate bucket fetches the bytes itself and calls DeriveVariants
// directly instead.
func (p *MediaPipeline) DeriveFromAttachment(ctx context.Context, companyID uuid.UUID, a upload.Attachment) (Media, error) {
	if p.storage == nil {
		return Media{}, ErrMediaStorageDisabled
	}
	body, obj, err := p.storage.Get(ctx, a.StorageKey)
	if err != nil {
		return Media{}, fmt.Errorf("cms: fetch attachment %q: %w", a.StorageKey, err)
	}
	defer body.Close()

	data, err := io.ReadAll(io.LimitReader(body, p.maxBytes+1))
	if err != nil {
		return Media{}, fmt.Errorf("cms: read attachment %q: %w", a.StorageKey, err)
	}
	contentType := a.ContentType
	if contentType == "" {
		contentType = obj.ContentType
	}
	return p.DeriveVariants(ctx, companyID, UploadInput{
		Data:        data,
		ContentType: contentType,
		Filename:    a.Filename,
	})
}

// PresignVariant returns a fresh presigned GET URL for one already-derived
// variant's StorageKey, valid for the pipeline's configured URLTTL.
func (p *MediaPipeline) PresignVariant(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if p.storage == nil {
		return "", ErrMediaStorageDisabled
	}
	if ttl == 0 {
		ttl = p.urlTTL
	}
	url, err := p.storage.PresignedGet(ctx, key, ttl)
	if err != nil {
		return "", fmt.Errorf("cms: presign variant: %w", err)
	}
	return url, nil
}

// variantKey returns the deterministic, content-addressed storage key for
// (companyID, source content hash, variant name). This is the single
// source of the "idempotent -- same source yields same keys" property:
// it is a pure function of its three inputs, with no timestamp, random
// suffix, or upload-order dependency.
func (p *MediaPipeline) variantKey(companyID uuid.UUID, hashHex, variantName string) string {
	return fmt.Sprintf("%s/%s/%s/%s", p.keyPrefix, companyID, hashHex, variantName)
}

// metaDims recovers width/height from an Object's Metadata map, as
// written by DeriveVariants' Put calls. Lookups tolerate a few common
// casings since some object-store backends normalize user-metadata key
// case in transit; an absent or unparseable value yields 0, never an
// error -- these dimensions are informational, not load-bearing for
// correctness.
func metaDims(meta map[string]string) (width, height int) {
	for _, k := range []string{"width", "Width", "WIDTH"} {
		if v, ok := meta[k]; ok {
			width, _ = strconv.Atoi(v)
			break
		}
	}
	for _, k := range []string{"height", "Height", "HEIGHT"} {
		if v, ok := meta[k]; ok {
			height, _ = strconv.Atoi(v)
			break
		}
	}
	return width, height
}

// resizeToWidth returns a copy of src downscaled so its width equals
// targetWidth, preserving aspect ratio (height rounds, minimum 1px).
// Resampling is nearest-neighbor -- this package deliberately avoids
// adding golang.org/x/image/draw as a new module dependency (go.mod is
// outside this TRD's file ownership); nearest-neighbor is sufficient for
// the thumbnail/display renditions this pipeline derives, using only the
// standard library's image package.
//
// Callers only ever invoke this to downscale (DeriveVariants checks
// cfg.Width > v.Width before calling it), so targetWidth < src's width
// on every real call.
func resizeToWidth(src image.Image, targetWidth int) *image.RGBA {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if targetWidth <= 0 || srcW <= 0 {
		targetWidth = srcW
	}
	targetHeight := srcH
	if srcW > 0 {
		targetHeight = int(float64(srcH) * float64(targetWidth) / float64(srcW))
	}
	if targetHeight < 1 {
		targetHeight = 1
	}
	if targetWidth < 1 {
		targetWidth = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	for y := 0; y < targetHeight; y++ {
		sy := b.Min.Y + y*srcH/targetHeight
		for x := 0; x < targetWidth; x++ {
			sx := b.Min.X + x*srcW/targetWidth
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
