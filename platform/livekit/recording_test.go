package livekit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	livekitproto "github.com/livekit/protocol/livekit"
)

// fakeEgress records every call and lets tests inject errors / canned responses.
type fakeEgress struct {
	startReq    *livekitproto.RoomCompositeEgressRequest
	stopReq     *livekitproto.StopEgressRequest
	startResult *livekitproto.EgressInfo
	startErr    error
	stopErr     error
}

func (f *fakeEgress) StartRoomCompositeEgress(_ context.Context, req *livekitproto.RoomCompositeEgressRequest) (*livekitproto.EgressInfo, error) {
	f.startReq = req
	if f.startErr != nil {
		return nil, f.startErr
	}
	if f.startResult != nil {
		return f.startResult, nil
	}
	return &livekitproto.EgressInfo{EgressId: "egress-fake-id"}, nil
}

func (f *fakeEgress) StopEgress(_ context.Context, req *livekitproto.StopEgressRequest) (*livekitproto.EgressInfo, error) {
	f.stopReq = req
	if f.stopErr != nil {
		return nil, f.stopErr
	}
	return &livekitproto.EgressInfo{EgressId: req.EgressId}, nil
}

func validS3() S3Config {
	return S3Config{
		Endpoint:       "http://minio:9000",
		AccessKey:      "AK",
		SecretKey:      "SK",
		Region:         "us-east-1",
		Bucket:         "recordings",
		ForcePathStyle: true,
	}
}

func TestS3Config_Validate(t *testing.T) {
	good := validS3()
	if err := good.Validate(); err != nil {
		t.Fatalf("good config: %v", err)
	}
	bad := good
	bad.Bucket = ""
	if !errors.Is(bad.Validate(), ErrConfig) {
		t.Error("missing bucket should fail")
	}
	bad = good
	bad.AccessKey = ""
	if !errors.Is(bad.Validate(), ErrConfig) {
		t.Error("missing access key should fail")
	}
}

func TestNewRecordingService_Validates(t *testing.T) {
	store := NewInMemoryStore()
	egress := &fakeEgress{}
	if _, err := NewRecordingService(nil, egress, RecordingConfig{S3: validS3()}, nil); !errors.Is(err, ErrConfig) {
		t.Errorf("nil store: %v", err)
	}
	if _, err := NewRecordingService(store, nil, RecordingConfig{S3: validS3()}, nil); !errors.Is(err, ErrConfig) {
		t.Errorf("nil egress: %v", err)
	}
	if _, err := NewRecordingService(store, egress, RecordingConfig{}, nil); !errors.Is(err, ErrConfig) {
		t.Errorf("missing s3: %v", err)
	}
	if _, err := NewRecordingService(store, egress, RecordingConfig{S3: validS3()}, nil); err != nil {
		t.Errorf("good config: %v", err)
	}
}

func TestStartRecording_HappyPath(t *testing.T) {
	store := NewInMemoryStore()
	egress := &fakeEgress{startResult: &livekitproto.EgressInfo{EgressId: "egress-001"}}
	clock := NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	rs, _ := NewRecordingService(store, egress, RecordingConfig{S3: validS3()}, clock)

	meetingID := uuid.New()
	rec, err := rs.StartRecording(context.Background(), meetingID, "meeting-room")
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	if rec.EgressID != "egress-001" {
		t.Errorf("EgressID = %q", rec.EgressID)
	}
	if rec.State != RecordingStateRecording {
		t.Errorf("state = %s", rec.State)
	}
	if rec.MeetingID != meetingID {
		t.Errorf("meeting id mismatch")
	}
	if egress.startReq.RoomName != "meeting-room" {
		t.Errorf("RoomName = %q", egress.startReq.RoomName)
	}
	if egress.startReq.Layout != "speaker" {
		t.Errorf("Layout = %q", egress.startReq.Layout)
	}
	file := egress.startReq.GetFile()
	if file == nil || file.Filepath != "recordings/meeting-room/{time}.mp4" {
		t.Errorf("Filepath = %v", file)
	}
}

func TestStartRecording_CustomTemplate(t *testing.T) {
	store := NewInMemoryStore()
	egress := &fakeEgress{}
	rs, _ := NewRecordingService(store, egress, RecordingConfig{
		S3:               validS3(),
		Layout:           "grid",
		FilePathTemplate: "custom/{room}-recording.mp4",
	}, nil)
	if _, err := rs.StartRecording(context.Background(), uuid.New(), "abc"); err != nil {
		t.Fatal(err)
	}
	if egress.startReq.Layout != "grid" {
		t.Errorf("layout = %q", egress.startReq.Layout)
	}
	if egress.startReq.GetFile().Filepath != "custom/abc-recording.mp4" {
		t.Errorf("filepath = %q", egress.startReq.GetFile().Filepath)
	}
}

func TestStopRecording_HappyPath(t *testing.T) {
	store := NewInMemoryStore()
	egress := &fakeEgress{}
	clock := NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	rs, _ := NewRecordingService(store, egress, RecordingConfig{S3: validS3()}, clock)

	rec, _ := rs.StartRecording(context.Background(), uuid.New(), "room-x")
	clock.Advance(2 * time.Minute)

	updated, err := rs.StopRecording(context.Background(), rec.ID)
	if err != nil {
		t.Fatalf("StopRecording: %v", err)
	}
	if updated.State != RecordingStateProcessing {
		t.Errorf("state = %s", updated.State)
	}
	if updated.EndedAt == nil {
		t.Error("EndedAt nil")
	}
	if egress.stopReq == nil || egress.stopReq.EgressId != rec.EgressID {
		t.Errorf("stopReq = %+v", egress.stopReq)
	}
}

func TestStopRecording_RejectsAlreadyStopped(t *testing.T) {
	store := NewInMemoryStore()
	egress := &fakeEgress{}
	rs, _ := NewRecordingService(store, egress, RecordingConfig{S3: validS3()}, nil)
	rec, _ := rs.StartRecording(context.Background(), uuid.New(), "room-x")
	if _, err := rs.StopRecording(context.Background(), rec.ID); err != nil {
		t.Fatal(err)
	}
	_, err := rs.StopRecording(context.Background(), rec.ID)
	if !errors.Is(err, ErrInvalidState) {
		t.Errorf("want ErrInvalidState, got %v", err)
	}
}

func TestHandleEgressEnded_Completed(t *testing.T) {
	store := NewInMemoryStore()
	egress := &fakeEgress{}
	clock := NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	rs, _ := NewRecordingService(store, egress, RecordingConfig{S3: validS3()}, clock)
	rec, _ := rs.StartRecording(context.Background(), uuid.New(), "room-x")

	const sixMinutesNs = 6 * 60 * 1_000_000_000
	if err := rs.HandleEgressEnded(context.Background(), rec.EgressID, "s3://recordings/abc.mp4", 1024*1024*50, sixMinutesNs, false); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetRecording(context.Background(), rec.ID)
	if stored.State != RecordingStateCompleted {
		t.Errorf("state = %s", stored.State)
	}
	if stored.FilePath != "s3://recordings/abc.mp4" {
		t.Errorf("filepath = %q", stored.FilePath)
	}
	if stored.FileSize != 1024*1024*50 {
		t.Errorf("filesize = %d", stored.FileSize)
	}
	if stored.Duration != 6*time.Minute {
		t.Errorf("duration = %v", stored.Duration)
	}
	if stored.EndedAt == nil {
		t.Error("EndedAt nil")
	}
}

func TestHandleEgressEnded_Failed(t *testing.T) {
	store := NewInMemoryStore()
	egress := &fakeEgress{}
	rs, _ := NewRecordingService(store, egress, RecordingConfig{S3: validS3()}, nil)
	rec, _ := rs.StartRecording(context.Background(), uuid.New(), "room-x")

	if err := rs.HandleEgressEnded(context.Background(), rec.EgressID, "", 0, 0, true); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetRecording(context.Background(), rec.ID)
	if stored.State != RecordingStateFailed {
		t.Errorf("state = %s, want failed", stored.State)
	}
}

func TestHandleEgressEnded_UnknownEgressID(t *testing.T) {
	store := NewInMemoryStore()
	egress := &fakeEgress{}
	rs, _ := NewRecordingService(store, egress, RecordingConfig{S3: validS3()}, nil)
	err := rs.HandleEgressEnded(context.Background(), "unknown", "", 0, 0, false)
	if !errors.Is(err, ErrRecordingNotFound) {
		t.Errorf("want ErrRecordingNotFound, got %v", err)
	}
}
