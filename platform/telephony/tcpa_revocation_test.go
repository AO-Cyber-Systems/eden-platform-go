package telephony

// Ported from politihub/go/internal/telephony/tcpa_revocation_test.go
// (Objective 40 TRD 40-05) — Obj 47 TRD 47-03 (c)'s statutory-word coverage
// for 47 CFR § 64.1200(a)(10), carried over because the same statute
// applies regardless of tenant.
//
// TWO DELIBERATE DEVIATIONS FROM THE SOURCE FILE:
//
//  1. Package: politihub's version is `package telephony_test` (external,
//     black-box) with its own private-schema-clone harness, because that
//     TRD ran against a persistent shared database whose `public` schema
//     was read-only to the test suite. This repo's telephony package tests
//     (config_store_test.go and this file) all use `package telephony`
//     (internal) against a DATABASE_URL-gated ephemeral database via
//     pgstore.NewBackend, which applies every migration fresh. Reusing that
//     existing convention (mustCreateCompany, DATABASE_URL) is simpler than
//     porting a schema-clone harness that exists to solve a problem this
//     repo's test setup does not have.
//  2. The end-to-end sub-test is extended with a THIRD case beyond
//     politihub's two (revoke, stop_control): a STOP -> START -> STOP
//     ordering case, proving this objective's own must-have ("a STOP after
//     a START opts the number back out") against the real Postgres store,
//     not just the in-memory fake tcpa_test.go already covers.
//
// WHAT IS BROKEN, IN PRODUCTION, IN THE UPSTREAM SOURCE THIS WAS PORTED
// FROM (politihub, before its own TRD 47-03).
//
// 47 CFR § 64.1200(a)(10) names seven per se reasonable revocation replies:
// "stop," "quit," "end," "revoke," "opt out," "cancel," "unsubscribe." Six of
// them matched — five through exactStopKeywords and "opt out" through the
// fuzzy path ("opt out" ∈ fuzzyPhraseTokens, "opt" ∈ stopIndicatorWords).
// "revoke" matched NOTHING before that TRD's fix — carried over here already
// fixed (see tcpa.go's exactStopKeywords / fuzzyPhraseTokens / stopIndicatorWords).
//
// WHY THE LOOP COVERS ALL SEVEN AND NOT JUST "revoke". Asserting only
// "revoke" would stay green while a refactor silently dropped "quit". The
// list below is literal and complete, so a regression anywhere in the
// statutory set fails here.
//
// WHY THE NEGATIVE CONTROLS ARE NOT DECORATION. Suppressing somebody who did
// not ask is worse than suppressing nobody: it silences a recipient who
// never opted out while the sender believes its list is clean. "I'll stop
// by tomorrow" and "can you send that again" must still match NOTHING after
// the change, in the same run as the seven positives. The fuzzy path's
// standalone-token semantics are what keep this safe — "revoke" must never
// become a bare substring test, or "she revoked my earlier RSVP" starts
// suppressing people.
//
// NO FCC EFFECTIVE DATE IS ASSERTED OR ENCODED. See tcpa.go's comment on the
// revoke entries for the posture this carries over from politihub's RULE-04.

import (
	"context"
	"testing"
)

// revStatutoryWords is 47 CFR § 64.1200(a)(10)'s list, verbatim and
// complete. Read via Cornell LII — MEDIUM confidence, not legal advice. Do
// not narrow this list to a single word; the point is that a regression in
// any of the other six fails here too.
var revStatutoryWords = []string{
	"stop",
	"quit",
	"end",
	"revoke",
	"opt out",
	"cancel",
	"unsubscribe",
}

// revNegativeBodies must match NOTHING. Both are ordinary conversational
// replies. The first contains the literal token "stop"; the second is a
// plain request to resend. If either starts matching, the matcher became
// promiscuous.
var revNegativeBodies = []string{
	"I'll stop by tomorrow",
	"can you send that again",
}

func TestStopRevocation(t *testing.T) {
	// -- Case 4: all seven statutory words -----------------------------------
	t.Run("all_seven_statutory_words_match", func(t *testing.T) {
		for _, w := range revStatutoryWords {
			w := w
			t.Run(w, func(t *testing.T) {
				matched, reason := FuzzyMatchStop(w)
				t.Logf("STATUTORY_WORD [%s] matched=%v reason=%s", w, matched, reason)
				if !matched {
					t.Errorf("47 CFR 64.1200(a)(10) names %q as a per se reasonable revocation reply, and it matches nothing", w)
				}
			})
		}
	})

	// -- Case 4b: the statutory words survive normalization ------------------
	// The provider delivers whatever the human typed. Uppercase and trailing
	// punctuation are the two commonest spellings and normalizeBody already
	// handles both; asserting it here means a change to the normalizer that
	// broke them fails in the compliance suite rather than in production.
	t.Run("statutory_words_survive_normalization", func(t *testing.T) {
		for _, tc := range []string{"REVOKE", "Revoke.", "  revoke  ", "STOP", "Opt Out"} {
			matched, _ := FuzzyMatchStop(tc)
			t.Logf("STATUTORY_SPELLING [%s] matched=%v", tc, matched)
			if !matched {
				t.Errorf("spelling %q of a statutory revocation reply matched nothing", tc)
			}
		}
	})

	// -- Case 5: the negative controls, same run -----------------------------
	t.Run("matcher_did_not_become_promiscuous", func(t *testing.T) {
		for _, body := range revNegativeBodies {
			matched, reason := FuzzyMatchStop(body)
			t.Logf("NEGATIVE_BODY [%s] matched=%v reason=%s", body, matched, reason)
			if matched {
				t.Errorf("ordinary reply %q was read as an opt-out; suppressing somebody who did not ask silences a recipient while the sender believes its list is clean", body)
			}
		}

		// The substring trap, stated explicitly. These contain "revoke" as a
		// SUBSTRING and are not opt-outs. If "revoke" is ever added as a bare
		// strings.Contains test without the standalone-indicator requirement,
		// these start matching.
		for _, body := range []string{
			"that clause is irrevocable",
			"she revoked my earlier RSVP",
		} {
			matched, _ := FuzzyMatchStop(body)
			t.Logf("NEGATIVE_SUBSTRING [%s] matched=%v", body, matched)
			if matched {
				t.Errorf("%q matched: revoke was added as a substring test rather than a standalone token", body)
			}
		}

		// POSITIVE CONTROL ON THE SAME PATH. Without it, every negative above
		// is satisfied by a matcher that matches nothing at all — which is
		// exactly the pre-fix behaviour for "revoke" these negatives are meant
		// to survive, not to certify.
		ctrl, _ := FuzzyMatchStop("please revoke my consent")
		t.Logf("NEGATIVE_CONTROL [please revoke my consent] matched=%v", ctrl)
		if !ctrl {
			t.Errorf("the negative controls are vacuous: an explicit revocation sentence does not match either")
		}
	})

	// -- Case 6: end to end through HandleInboundBody ------------------------
	// A matcher entry is only worth anything if it reaches the durable,
	// phone-keyed record that gates outbound sends. This drives the real
	// TCPAService over the real Postgres store and counts rows.
	//
	// DATABASE_URL-gated (skipped when unset) — a local skip is not a pass.
	t.Run("inbound_revoke_writes_a_phone_optout_row", func(t *testing.T) {
		backend, authStore := setupConfigStoreTest(t)
		store := NewPostgresPhoneOptOutStore(backend.Pool())
		svc := NewTCPAService(nil, nil, store)
		ctx := context.Background()

		for _, tc := range []struct{ label, phone, body string }{
			{"revoke", "+12075550171", "REVOKE"},
			{"stop_control", "+12075550172", "STOP"},
		} {
			company := mustCreateCompany(t, authStore, "rev-"+tc.label)
			matched, err := svc.HandleInboundBody(ctx, company, ProviderTwilio,
				InboundSMS{MessageSID: "rev-" + tc.label, From: tc.phone, To: "+12075550100", Body: tc.body})
			if err != nil {
				t.Fatalf("HandleInboundBody(%s): %v", tc.label, err)
			}
			optedOut, err := store.IsOptedOut(ctx, company, tc.phone)
			if err != nil {
				t.Fatalf("IsOptedOut(%s): %v", tc.label, err)
			}
			t.Logf("INBOUND_E2E [%s] body=%q matched=%v opted_out=%v", tc.label, tc.body, matched, optedOut)
			if !matched {
				t.Errorf("inbound %q was not read as an opt-out", tc.body)
			}
			if !optedOut {
				t.Errorf("inbound %q did not produce a durable opt-out row", tc.body)
			}
		}
	})

	// -- Case 7 (new — this TRD's must-have, not in the politihub source):
	// a STOP after a START opts the number back out, proved against the
	// real Postgres store and unique index, not just the in-memory fake in
	// tcpa_test.go's TestTCPAService_RevocationOrdering.
	t.Run("revocation_ordering_stop_start_stop", func(t *testing.T) {
		backend, authStore := setupConfigStoreTest(t)
		store := NewPostgresPhoneOptOutStore(backend.Pool())
		svc := NewTCPAService(nil, nil, store)
		ctx := context.Background()

		company := mustCreateCompany(t, authStore, "rev-ordering")
		phone := "+12075550199"

		matched, err := svc.HandleInboundBody(ctx, company, ProviderTwilio, InboundSMS{From: phone, Body: "STOP"})
		if err != nil || !matched {
			t.Fatalf("first STOP: matched=%v err=%v", matched, err)
		}
		if optedOut, _ := store.IsOptedOut(ctx, company, phone); !optedOut {
			t.Fatal("expected opted out after first STOP")
		}

		matched, err = svc.HandleInboundBody(ctx, company, ProviderTwilio, InboundSMS{From: phone, Body: "START"})
		if err != nil || !matched {
			t.Fatalf("START: matched=%v err=%v", matched, err)
		}
		if optedOut, _ := store.IsOptedOut(ctx, company, phone); optedOut {
			t.Fatal("expected opted back in after START")
		}

		matched, err = svc.HandleInboundBody(ctx, company, ProviderTwilio, InboundSMS{From: phone, Body: "STOP"})
		if err != nil || !matched {
			t.Fatalf("second STOP: matched=%v err=%v", matched, err)
		}
		if optedOut, _ := store.IsOptedOut(ctx, company, phone); !optedOut {
			t.Fatal("expected opted out again after second STOP — revocation was not honoured, and the unique index would otherwise have blocked the re-insert")
		}

		// CheckSendAllowed must refuse a send at this final state — the
		// precondition, not a post-filter.
		if err := svc.CheckSendAllowed(ctx, company, phone); err == nil {
			t.Fatal("CheckSendAllowed should refuse a send to a number opted out again after the STOP->START->STOP sequence")
		}
	})
}
