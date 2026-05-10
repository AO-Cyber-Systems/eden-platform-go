package livekit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	livekitproto "github.com/livekit/protocol/livekit"
)

// S3Config is the S3-compatible storage destination for egress output. It
// purposely matches LiveKit's S3Upload spec — when the platform/storage
// package (Objective 19) lands, we expect to grow a Storage interface that
// produces this from a more abstract config.
type S3Config struct {
	Endpoint       string
	AccessKey      string
	SecretKey      string
	Region         string
	Bucket         string
	ForcePathStyle bool // required for MinIO and other path-style hosts
}

// Validate reports configuration issues that would surface as errors during
// StartRoomCompositeEgress.
func (c S3Config) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("%w: S3Config.Bucket is required", ErrConfig)
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		return fmt.Errorf("%w: S3Config credentials are required", ErrConfig)
	}
	return nil
}

// RecordingConfig holds the egress destination for a RecordingService.
//
// Future versions may grow a Storage interface here so callers can plug in
// platform/storage instead of raw S3.
type RecordingConfig struct {
	S3 S3Config

	// Layout overrides the default speaker layout for room composite
	// egress. Empty falls back to "speaker".
	Layout string

	// FilePathTemplate is the egress filepath template. Empty falls back
	// to "recordings/{room}/{time}.mp4". The literal "{room}" is replaced
	// with the room name; LiveKit substitutes "{time}" server-side.
	FilePathTemplate string
}

// RecordingService manages meeting recordings via LiveKit Egress.
type RecordingService struct {
	store  Store
	egress EgressClient
	cfg    RecordingConfig
	clock  Clock
}

// NewRecordingService validates cfg and constructs a RecordingService.
func NewRecordingService(store Store, egress EgressClient, cfg RecordingConfig, clock Clock) (*RecordingService, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: Store is required", ErrConfig)
	}
	if egress == nil {
		return nil, fmt.Errorf("%w: EgressClient is required", ErrConfig)
	}
	if err := cfg.S3.Validate(); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = NewRealClock()
	}
	return &RecordingService{store: store, egress: egress, cfg: cfg, clock: clock}, nil
}

// StartRecording starts a room composite recording for a meeting and persists
// a Recording row in the recording state.
func (rs *RecordingService) StartRecording(ctx context.Context, meetingID uuid.UUID, roomName string) (Recording, error) {
	layout := rs.cfg.Layout
	if layout == "" {
		layout = "speaker"
	}
	template := rs.cfg.FilePathTemplate
	if template == "" {
		template = "recordings/{room}/{time}.mp4"
	}
	filepath := strings.ReplaceAll(template, "{room}", roomName)

	req := &livekitproto.RoomCompositeEgressRequest{
		RoomName: roomName,
		Layout:   layout,
		Output: &livekitproto.RoomCompositeEgressRequest_File{
			File: &livekitproto.EncodedFileOutput{
				FileType: livekitproto.EncodedFileType_MP4,
				Filepath: filepath,
				Output: &livekitproto.EncodedFileOutput_S3{
					S3: &livekitproto.S3Upload{
						AccessKey:      rs.cfg.S3.AccessKey,
						Secret:         rs.cfg.S3.SecretKey,
						Region:         rs.cfg.S3.Region,
						Endpoint:       rs.cfg.S3.Endpoint,
						Bucket:         rs.cfg.S3.Bucket,
						ForcePathStyle: rs.cfg.S3.ForcePathStyle,
					},
				},
			},
		},
	}
	info, err := rs.egress.StartRoomCompositeEgress(ctx, req)
	if err != nil {
		return Recording{}, fmt.Errorf("start room composite egress: %w", err)
	}

	rec := Recording{
		ID:        uuid.New(),
		MeetingID: meetingID,
		EgressID:  info.GetEgressId(),
		State:     RecordingStateRecording,
		StartedAt: rs.clock.Now(),
	}
	stored, err := rs.store.CreateRecording(ctx, rec)
	if err != nil {
		return Recording{}, fmt.Errorf("create recording: %w", err)
	}
	return stored, nil
}

// StopRecording stops an active recording and transitions it to processing.
// The webhook handler finalises the metadata when egress_ended fires.
func (rs *RecordingService) StopRecording(ctx context.Context, recordingID uuid.UUID) (Recording, error) {
	rec, err := rs.store.GetRecording(ctx, recordingID)
	if err != nil {
		return Recording{}, fmt.Errorf("get recording: %w", err)
	}
	if rec.State != RecordingStateRecording {
		return Recording{}, fmt.Errorf("%w: recording is %s, not recording", ErrInvalidState, rec.State)
	}
	if _, err := rs.egress.StopEgress(ctx, &livekitproto.StopEgressRequest{EgressId: rec.EgressID}); err != nil {
		return Recording{}, fmt.Errorf("stop egress: %w", err)
	}
	now := rs.clock.Now()
	rec.State = RecordingStateProcessing
	rec.EndedAt = &now
	updated, err := rs.store.UpdateRecording(ctx, rec)
	if err != nil {
		return Recording{}, fmt.Errorf("update recording: %w", err)
	}
	return updated, nil
}

// HandleEgressEnded finalises a recording after LiveKit publishes
// egress_ended. durationNs is the duration in nanoseconds (matching LiveKit's
// EgressInfo.FileResults[].Duration).
func (rs *RecordingService) HandleEgressEnded(ctx context.Context, egressID, filePath string, fileSize int64, durationNs int64, failed bool) error {
	rec, err := rs.store.GetRecordingByEgressID(ctx, egressID)
	if err != nil {
		return fmt.Errorf("get recording by egress: %w", err)
	}
	state := RecordingStateCompleted
	if failed {
		state = RecordingStateFailed
	}
	now := rs.clock.Now()
	rec.State = state
	if filePath != "" {
		rec.FilePath = filePath
	}
	if fileSize > 0 {
		rec.FileSize = fileSize
	}
	if durationNs > 0 {
		rec.Duration = time.Duration(durationNs)
	}
	rec.EndedAt = &now
	if _, err := rs.store.UpdateRecording(ctx, rec); err != nil {
		return fmt.Errorf("update recording: %w", err)
	}
	return nil
}

// ListRecordings returns recordings for a meeting (newest first).
func (rs *RecordingService) ListRecordings(ctx context.Context, meetingID uuid.UUID) ([]Recording, error) {
	return rs.store.ListRecordingsForMeeting(ctx, meetingID)
}
