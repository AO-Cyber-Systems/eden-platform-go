package telephony

// Ported from politihub/go/internal/telephony/tcpa_test.go (Objective 40
// TRD 40-05). Every case from the source file is preserved verbatim in
// intent; only identifiers changed for the domain-neutral rename (see
// tcpa.go's header comment) and the committee -> company rename. New tests
// below the ported section cover FuzzyMatchStart and CheckSendAllowed,
// neither of which exist in the politihub source — see tcpa.go's
// PhoneOptOutStore.Remove doc comment for the navigators reconciliation
// that motivated them.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// --- ported verbatim (renames only) -----------------------------------

func TestFuzzyMatchStop_ExactKeywords(t *testing.T) {
	cases := []string{"STOP", "stop", "Stop ", "STOP!", "stop.", "QUIT", "Unsubscribe", "End", "CANCEL", "stopall"}
	for _, c := range cases {
		matched, reason := FuzzyMatchStop(c)
		if !matched {
			t.Errorf("FuzzyMatchStop(%q): not matched", c)
			continue
		}
		if reason != MatchReasonExactKeyword {
			t.Errorf("FuzzyMatchStop(%q): reason=%s, want exact_keyword", c, reason)
		}
	}
}

func TestFuzzyMatchStop_FuzzyPhrases(t *testing.T) {
	cases := []string{
		"please stop",
		"stop texting me",
		"take me off the list, stop",
		"remove me from this stop list",
		"i want to unsubscribe",
		"unsubscribe me please",
		"opt out",
		"no more texts please stop",
	}
	for _, c := range cases {
		matched, reason := FuzzyMatchStop(c)
		if !matched {
			t.Errorf("FuzzyMatchStop(%q): not matched", c)
			continue
		}
		if reason != MatchReasonFuzzyPhrase {
			t.Errorf("FuzzyMatchStop(%q): reason=%s, want fuzzy_phrase", c, reason)
		}
	}
}

func TestFuzzyMatchStop_NonMatches(t *testing.T) {
	cases := []string{
		"unstoppable",
		"bootstrap",
		"hello",
		"thanks",
		"endless",
	}
	for _, c := range cases {
		matched, _ := FuzzyMatchStop(c)
		if matched {
			t.Errorf("FuzzyMatchStop(%q): matched but should not", c)
		}
	}
}

func TestFuzzyMatchHelp(t *testing.T) {
	yes := []string{"HELP", "help", "Help", "INFO", "info"}
	no := []string{"please help", "I need help", "more info please"}
	for _, c := range yes {
		if !FuzzyMatchHelp(c) {
			t.Errorf("FuzzyMatchHelp(%q): expected match", c)
		}
	}
	for _, c := range no {
		if FuzzyMatchHelp(c) {
			t.Errorf("FuzzyMatchHelp(%q): unexpected match", c)
		}
	}
}

// fakeRecipientLookup / fakeConsents / fakePhoneStore for HandleInboundBody.
type fakeRecipientLookup struct {
	id    uuid.UUID
	found bool
	err   error
}

func (f fakeRecipientLookup) FindRecipientByPhone(_ context.Context, _ uuid.UUID, _ string) (uuid.UUID, bool, error) {
	return f.id, f.found, f.err
}

type fakeConsents struct {
	calls   []string
	failErr error
}

func (f *fakeConsents) RecordOptOut(_ context.Context, _, _ uuid.UUID, channel, source string) error {
	if f.failErr != nil {
		return f.failErr
	}
	f.calls = append(f.calls, channel+":"+source)
	return nil
}

type fakePhoneStore struct {
	upserts       []string
	removed       []string
	optedOut      map[string]bool
	failErr       error
	isOptedOutErr error
}

func (f *fakePhoneStore) Upsert(_ context.Context, _ uuid.UUID, phone, reason, via, _ string) error {
	if f.failErr != nil {
		return f.failErr
	}
	f.upserts = append(f.upserts, phone+":"+reason+":"+via)
	if f.optedOut == nil {
		f.optedOut = map[string]bool{}
	}
	f.optedOut[phone] = true
	return nil
}

func (f *fakePhoneStore) IsOptedOut(_ context.Context, _ uuid.UUID, phone string) (bool, error) {
	if f.isOptedOutErr != nil {
		return false, f.isOptedOutErr
	}
	return f.optedOut[phone], nil
}

func (f *fakePhoneStore) Remove(_ context.Context, _ uuid.UUID, phone string) error {
	if f.failErr != nil {
		return f.failErr
	}
	f.removed = append(f.removed, phone)
	if f.optedOut != nil {
		delete(f.optedOut, phone)
	}
	return nil
}

func TestTCPAService_HandleInboundBody_StopMatch(t *testing.T) {
	recipientID := uuid.New()
	consents := &fakeConsents{}
	phones := &fakePhoneStore{}
	svc := NewTCPAService(fakeRecipientLookup{id: recipientID, found: true}, consents, phones)

	company := uuid.New()
	matched, err := svc.HandleInboundBody(context.Background(), company, ProviderTwilio, InboundSMS{
		From: "+15555550111", Body: "STOP",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !matched {
		t.Fatal("expected matched")
	}
	if got := len(phones.upserts); got != 1 {
		t.Errorf("phone upserts = %d; want 1", got)
	}
	if got := len(consents.calls); got != 1 {
		t.Errorf("consent calls = %d; want 1", got)
	}
}

func TestTCPAService_HandleInboundBody_NoMatch(t *testing.T) {
	consents := &fakeConsents{}
	phones := &fakePhoneStore{}
	svc := NewTCPAService(fakeRecipientLookup{found: true}, consents, phones)
	matched, err := svc.HandleInboundBody(context.Background(), uuid.New(), ProviderTwilio, InboundSMS{Body: "hello"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if matched {
		t.Error("expected no match")
	}
	if len(phones.upserts) != 0 || len(consents.calls) != 0 {
		t.Errorf("unexpected writes: phones=%d consents=%d", len(phones.upserts), len(consents.calls))
	}
}

func TestTCPAService_HandleInboundBody_PhoneOnly(t *testing.T) {
	consents := &fakeConsents{}
	phones := &fakePhoneStore{}
	// Recipient not found — phone-only opt-out path.
	svc := NewTCPAService(fakeRecipientLookup{found: false}, consents, phones)
	matched, err := svc.HandleInboundBody(context.Background(), uuid.New(), ProviderSignalWire, InboundSMS{
		From: "+15555550111", Body: "please stop",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !matched {
		t.Fatal("expected matched")
	}
	if len(phones.upserts) != 1 {
		t.Errorf("phone upserts = %d; want 1", len(phones.upserts))
	}
	if len(consents.calls) != 0 {
		t.Errorf("consent calls = %d; want 0 (no recipient linked)", len(consents.calls))
	}
}

func TestTCPAService_HandleInboundBody_PhoneStoreFailure(t *testing.T) {
	phones := &fakePhoneStore{failErr: errors.New("db down")}
	svc := NewTCPAService(fakeRecipientLookup{found: false}, &fakeConsents{}, phones)
	matched, err := svc.HandleInboundBody(context.Background(), uuid.New(), ProviderTwilio, InboundSMS{From: "+1", Body: "STOP"})
	if !matched {
		t.Error("matched should be true even when store fails")
	}
	if err == nil {
		t.Error("expected error from phone store failure")
	}
}

func TestTCPAService_NilStoresAreSafe(t *testing.T) {
	svc := NewTCPAService(nil, nil, nil)
	matched, err := svc.HandleInboundBody(context.Background(), uuid.New(), ProviderTwilio, InboundSMS{Body: "STOP"})
	if err != nil {
		t.Errorf("nil stores should not error: %v", err)
	}
	if !matched {
		t.Error("matched should be true even with nil stores (matcher still ran)")
	}
}

// --- new coverage: FuzzyMatchStart (navigators reconciliation) --------

func TestFuzzyMatchStart_ExactKeywords(t *testing.T) {
	cases := []string{"START", "start", "Start ", "START!", "UNSTOP", "unstop", "Unstop."}
	for _, c := range cases {
		matched, reason := FuzzyMatchStart(c)
		if !matched {
			t.Errorf("FuzzyMatchStart(%q): not matched", c)
			continue
		}
		if reason != MatchReasonExactKeyword {
			t.Errorf("FuzzyMatchStart(%q): reason=%s, want exact_keyword", c, reason)
		}
	}
}

func TestFuzzyMatchStart_FuzzyPhrases(t *testing.T) {
	cases := []string{
		"please start sending texts again",
		"sign me back up",
		"sign me up again please",
		"opt me back in",
		"can you opt back in",
		"text me again",
		"i want to resubscribe",
	}
	for _, c := range cases {
		matched, reason := FuzzyMatchStart(c)
		if !matched {
			t.Errorf("FuzzyMatchStart(%q): not matched", c)
			continue
		}
		if reason != MatchReasonFuzzyPhrase {
			t.Errorf("FuzzyMatchStart(%q): reason=%s, want fuzzy_phrase", c, reason)
		}
	}
}

// TestFuzzyMatchStart_NonMatches is the START-path analogue of
// TestFuzzyMatchStop_NonMatches: ordinary conversational sentences that
// happen to contain "start" or "back" as a substring or standalone word
// must not opt a number back in.
func TestFuzzyMatchStart_NonMatches(t *testing.T) {
	cases := []string{
		"we're off to a good start",
		"i'll be back later",
		"starting the car now",
		"hello",
		"stop",
	}
	for _, c := range cases {
		matched, _ := FuzzyMatchStart(c)
		if matched {
			t.Errorf("FuzzyMatchStart(%q): matched but should not", c)
		}
	}
}

func TestTCPAService_HandleInboundBody_StartMatch(t *testing.T) {
	phones := &fakePhoneStore{optedOut: map[string]bool{"+15555550111": true}}
	svc := NewTCPAService(nil, nil, phones)

	matched, err := svc.HandleInboundBody(context.Background(), uuid.New(), ProviderTwilio, InboundSMS{
		From: "+15555550111", Body: "START",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !matched {
		t.Fatal("expected matched")
	}
	if len(phones.removed) != 1 || phones.removed[0] != "+15555550111" {
		t.Errorf("removed = %v; want [+15555550111]", phones.removed)
	}
	if phones.optedOut["+15555550111"] {
		t.Error("phone should no longer be opted out after START")
	}
}

func TestTCPAService_HandleInboundBody_StartStoreFailure(t *testing.T) {
	phones := &fakePhoneStore{failErr: errors.New("db down")}
	svc := NewTCPAService(nil, nil, phones)
	matched, err := svc.HandleInboundBody(context.Background(), uuid.New(), ProviderTwilio, InboundSMS{From: "+1", Body: "START"})
	if !matched {
		t.Error("matched should be true even when store fails")
	}
	if err == nil {
		t.Error("expected error from phone store failure")
	}
}

// TestTCPAService_RevocationOrdering is the in-memory analogue of this
// objective's must-have: "a STOP after a START opts the number back out".
// STOP -> START -> STOP must leave the phone opted out, not stuck in
// whatever state the middle transition produced. The Postgres-backed
// version of the same ordering lives in tcpa_revocation_test.go.
func TestTCPAService_RevocationOrdering(t *testing.T) {
	phones := &fakePhoneStore{}
	svc := NewTCPAService(nil, nil, phones)
	ctx := context.Background()
	company := uuid.New()
	phone := "+15555550111"

	matched, err := svc.HandleInboundBody(ctx, company, ProviderTwilio, InboundSMS{From: phone, Body: "STOP"})
	if err != nil || !matched {
		t.Fatalf("first STOP: matched=%v err=%v", matched, err)
	}
	if optedOut, _ := phones.IsOptedOut(ctx, company, phone); !optedOut {
		t.Fatal("expected opted out after first STOP")
	}

	matched, err = svc.HandleInboundBody(ctx, company, ProviderTwilio, InboundSMS{From: phone, Body: "START"})
	if err != nil || !matched {
		t.Fatalf("START: matched=%v err=%v", matched, err)
	}
	if optedOut, _ := phones.IsOptedOut(ctx, company, phone); optedOut {
		t.Fatal("expected opted back in after START")
	}

	matched, err = svc.HandleInboundBody(ctx, company, ProviderTwilio, InboundSMS{From: phone, Body: "STOP"})
	if err != nil || !matched {
		t.Fatalf("second STOP: matched=%v err=%v", matched, err)
	}
	if optedOut, _ := phones.IsOptedOut(ctx, company, phone); !optedOut {
		t.Fatal("expected opted out again after second STOP — revocation was not honoured")
	}
}

// --- new coverage: CheckSendAllowed (send precondition, not post-filter) --

func TestTCPAService_CheckSendAllowed_OptedOut(t *testing.T) {
	phones := &fakePhoneStore{optedOut: map[string]bool{"+15555550111": true}}
	svc := NewTCPAService(nil, nil, phones)

	err := svc.CheckSendAllowed(context.Background(), uuid.New(), "+15555550111")
	if !errors.Is(err, ErrRecipientOptedOut) {
		t.Fatalf("CheckSendAllowed: got %v, want ErrRecipientOptedOut", err)
	}
}

func TestTCPAService_CheckSendAllowed_NotOptedOut(t *testing.T) {
	phones := &fakePhoneStore{}
	svc := NewTCPAService(nil, nil, phones)

	if err := svc.CheckSendAllowed(context.Background(), uuid.New(), "+15555550111"); err != nil {
		t.Fatalf("CheckSendAllowed: unexpected error %v", err)
	}
}

// TestTCPAService_CheckSendAllowed_FailsClosed proves an opt-out-store
// error is treated as "not allowed" rather than "allowed" — an outage in
// the store must never look like consent.
func TestTCPAService_CheckSendAllowed_FailsClosed(t *testing.T) {
	phones := &fakePhoneStore{isOptedOutErr: errors.New("db down")}
	svc := NewTCPAService(nil, nil, phones)

	err := svc.CheckSendAllowed(context.Background(), uuid.New(), "+15555550111")
	if err == nil {
		t.Fatal("expected error (fail closed) when the opt-out store errors")
	}
	if errors.Is(err, ErrRecipientOptedOut) {
		t.Error("a store failure should surface as its own error, not be conflated with a confirmed opt-out")
	}
}

// TestTCPAService_CheckSendAllowed_IsPrecondition proves IsOptedOut gates a
// send attempt BEFORE anything is sent — a caller that checks
// CheckSendAllowed first never reaches a "send" step for an opted-out
// number, as opposed to sending unconditionally and filtering the result
// afterward.
func TestTCPAService_CheckSendAllowed_IsPrecondition(t *testing.T) {
	phones := &fakePhoneStore{optedOut: map[string]bool{"+15555550111": true}}
	svc := NewTCPAService(nil, nil, phones)

	sendAttempted := false
	send := func(phone string) error {
		if err := svc.CheckSendAllowed(context.Background(), uuid.New(), phone); err != nil {
			return err
		}
		sendAttempted = true
		return nil
	}

	if err := send("+15555550111"); !errors.Is(err, ErrRecipientOptedOut) {
		t.Fatalf("send: got %v, want ErrRecipientOptedOut", err)
	}
	if sendAttempted {
		t.Error("send must never be attempted for an opted-out number — the check is a precondition, not a post-filter")
	}
}
