package telephony

// TCPA-compliant inbound SMS keyword handling.
//
// Ported from politihub/go/internal/telephony/tcpa.go (Objective 40 TRD
// 40-05), adapted to be provider-neutral:
//
//   - VoterLookup -> RecipientLookup, FindVoterByPhone -> FindRecipientByPhone.
//     No voter concept enters this package -- a "recipient" is anything the
//     consuming application's own domain model links a phone number to.
//   - committeeID -> companyID, matching every other identifier in this
//     package (Config.CompanyID, ConfigStore.Get(ctx, companyID), ...).
//
// Algorithm reference (April 2025 FCC "any reasonable means" rule):
//
//  1. Normalize body: strip whitespace, lowercase, remove non-letter/
//     non-space runes.
//  2. Exact-keyword match: stop|quit|unsubscribe|end|cancel|stopall|revoke.
//  3. Fuzzy-phrase match: phrase substring AND a standalone stop-indicator
//     whole word (so "unstoppable" / "bootstrap" do NOT match).
//
// The same two-stage shape is reused below for the opt-back-in (START)
// path, which has NO equivalent in the politihub source -- see the note on
// FuzzyMatchStart and this objective's 40-05-SUMMARY.md for the
// navigators/sms_compliance.go reconciliation that motivated adding it.
import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// MatchReason identifies how an SMS keyword event was classified. Stored on
// the telephony_phone_optouts row for TCPA audit purposes.
type MatchReason string

// Match reasons.
const (
	MatchReasonExactKeyword MatchReason = "exact_keyword"
	MatchReasonFuzzyPhrase  MatchReason = "fuzzy_phrase"
)

// ErrRecipientOptedOut is returned by TCPAService.CheckSendAllowed when the
// destination phone has an active opt-out on file. Callers MUST treat this
// as a hard precondition failure and refuse the send -- see CheckSendAllowed.
var ErrRecipientOptedOut = errors.New("telephony tcpa: recipient has opted out")

// 47 CFR § 64.1200(a)(10) names the per se reasonable revocation replies:
// "stop," "quit," "end," "revoke," "opt out," "cancel," "unsubscribe."
//
// Six of the seven matched here from the start — five exactly, and "opt out"
// through the fuzzy path. "revoke" did not: a grep for it across this file
// returned nothing in the politihub source. Added there by Objective 47 TRD
// 47-03, which measured the gap; carried over unchanged here because the
// same statute applies regardless of tenant.
//
// The word is added in THREE places on purpose. exactStopKeywords catches
// the bare one-word reply; the fuzzy path requires BOTH a phrase token AND a
// standalone indicator word, so an entry in fuzzyPhraseTokens alone would do
// nothing and an entry in stopIndicatorWords alone would do nothing. The
// standalone-token semantics are what keep this safe: "that clause is
// irrevocable" and "she revoked my earlier RSVP" contain the substring and
// are NOT opt-outs. Never turn this into a bare substring test — suppressing
// somebody who did not ask silences a recipient while the sender believes
// its list is clean, which is worse than suppressing nobody.
//
// NO EFFECTIVE DATE IS ENCODED, deliberately, continuing the posture the
// politihub source recorded (its RULE-04): the revocation provisions other
// than the delayed "revoke-all across unrelated message categories" scope
// have been in force since spring of 2025; that delayed scope has moved
// more than once. A date literal in this file would silently change
// compliance behavior on a day nobody is watching. The conservative
// behavior ships; the date does not.
//
// exactStopKeywords is the set of normalized exact-match stop keywords.
var exactStopKeywords = map[string]struct{}{
	"stop":        {},
	"quit":        {},
	"unsubscribe": {},
	"end":         {},
	"cancel":      {},
	"stopall":     {},
	"revoke":      {},
}

// fuzzyPhraseTokens — phrases that, when found in the normalized body
// alongside a standalone stop-indicator word, trigger fuzzy opt-out.
var fuzzyPhraseTokens = []string{
	"stop texting",
	"stop messaging",
	"please stop",
	"stop me",
	"take me off",
	"remove me",
	"unsubscribe me",
	"unsubscribe",
	"no more texts",
	"no more messaging",
	"no more",
	"opt me out",
	"opt out",
	// 47 CFR § 64.1200(a)(10) — see the note on exactStopKeywords. Present
	// here so "please revoke my consent" reaches the fuzzy path; a phrase
	// token WITHOUT the matching indicator word below would never fire.
	"revoke",
}

// stopIndicatorWords are words that must appear as standalone tokens
// alongside a fuzzy phrase to confirm opt-out intent.
var stopIndicatorWords = map[string]struct{}{
	"stop":        {},
	"quit":        {},
	"unsubscribe": {},
	"end":         {},
	"cancel":      {},
	"remove":      {},
	"opt":         {},
	"off":         {},
	"no":          {},
	// 47 CFR § 64.1200(a)(10) — see the note on exactStopKeywords. This is
	// the standalone-token half of the pair: it is what stops the phrase
	// token above from matching "she revoked my earlier RSVP", where
	// "revoke" appears only as a substring and never as a word.
	"revoke": {},
}

// exactStartKeywords are the CTIA-documented opt-back-in replies. Unlike
// the STOP set, these are carrier/CTIA best-practice conventions, not an
// FCC-mandated statutory list — there is no 47 CFR analogue to
// § 64.1200(a)(10) for resubscription. "START" is the universally
// recognized keyword; "UNSTOP" is the common secondary alias. "YES" is
// deliberately excluded: it is conventionally reserved for INITIAL
// double-opt-in confirmation, not for reversing an existing STOP, and
// treating it as a resubscribe keyword would make an ordinary affirmative
// reply to an unrelated question opt a number back into messaging it had
// deliberately left.
var exactStartKeywords = map[string]struct{}{
	"start":  {},
	"unstop": {},
}

// fuzzyStartPhraseTokens mirrors fuzzyPhraseTokens' shape for the
// opt-back-in path: a phrase substring paired with a standalone indicator
// word below.
var fuzzyStartPhraseTokens = []string{
	"start again",
	"please start",
	"sign me back up",
	"sign me up again",
	"opt me back in",
	"opt back in",
	"text me again",
	"resubscribe",
}

// startIndicatorWords are words that must appear as standalone tokens
// alongside a fuzzy start phrase to confirm opt-back-in intent. Kept
// deliberately narrow: "back" and "again" alone are common in ordinary
// conversation, so they only count paired with one of the phrase tokens
// above, never on their own.
var startIndicatorWords = map[string]struct{}{
	"start":       {},
	"unstop":      {},
	"resubscribe": {},
	"back":        {},
	"again":       {},
}

// normalizeBody strips whitespace, lowercases, and removes non-letter/
// non-space runes.
func normalizeBody(body string) string {
	body = strings.TrimSpace(body)
	body = strings.ToLower(body)
	body = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsSpace(r) {
			return r
		}
		return -1
	}, body)
	return strings.TrimSpace(body)
}

// matchKeyword is the two-stage matcher shared by FuzzyMatchStop and
// FuzzyMatchStart: an exact-keyword check on the normalized body, then a
// fuzzy phrase-substring-plus-standalone-indicator-word check.
func matchKeyword(body string, exact map[string]struct{}, phrases []string, indicators map[string]struct{}) (bool, MatchReason) {
	normalized := normalizeBody(body)
	if _, ok := exact[normalized]; ok {
		return true, MatchReasonExactKeyword
	}

	words := strings.Fields(normalized)
	wordSet := make(map[string]struct{}, len(words))
	for _, w := range words {
		wordSet[w] = struct{}{}
	}

	for _, phrase := range phrases {
		if !strings.Contains(normalized, phrase) {
			continue
		}
		for indicator := range indicators {
			if _, has := wordSet[indicator]; has {
				return true, MatchReasonFuzzyPhrase
			}
		}
	}
	return false, ""
}

// FuzzyMatchStop returns (matched, reason) for the given message body.
//
// Stage 1 — exact keyword check on the normalized body.
// Stage 2 — fuzzy phrase: body contains a phrase token AND has a standalone
// stop-indicator word.
func FuzzyMatchStop(body string) (bool, MatchReason) {
	return matchKeyword(body, exactStopKeywords, fuzzyPhraseTokens, stopIndicatorWords)
}

// FuzzyMatchStart returns (matched, reason) for the given message body
// against the opt-back-in (START/UNSTOP) keyword set. Same two-stage shape
// as FuzzyMatchStop. See the exactStartKeywords doc comment for why this
// set is narrower than, and NOT sourced from, the same statute as STOP.
func FuzzyMatchStart(body string) (bool, MatchReason) {
	return matchKeyword(body, exactStartKeywords, fuzzyStartPhraseTokens, startIndicatorWords)
}

// FuzzyMatchHelp returns true when the normalized body is exactly "help" or
// "info". Strict by design: "I need help" does NOT trigger a HELP auto-reply.
func FuzzyMatchHelp(body string) bool {
	normalized := normalizeBody(body)
	return normalized == "help" || normalized == "info"
}

// RecipientLookup resolves a recipient ID for a phone number within a
// company. Returns (uuid.Nil, false, nil) when no recipient has the phone on
// file. This package has no opinion on what a "recipient" is beyond "an ID
// the consuming application's own domain model links to a phone number" —
// implement it as an adapter over whatever contact/member store that
// application already has.
type RecipientLookup interface {
	FindRecipientByPhone(ctx context.Context, companyID uuid.UUID, phone string) (uuid.UUID, bool, error)
}

// ConsentRecorder appends a consent record for a recipient linked by phone.
//
// Source is the provider name ('twilio' / 'signalwire' / ...) for audit
// trail purposes; channel is the consuming application's own consent
// channel (typically "sms").
type ConsentRecorder interface {
	RecordOptOut(ctx context.Context, companyID, recipientID uuid.UUID, channel, source string) error
}

// PhoneOptOutStore persists phone-keyed opt-outs (no linked recipient
// required). This is the durable, provider-neutral record that gates
// outbound sends — see TCPAService.CheckSendAllowed, which is the
// precondition an outbound send call site MUST consult before ever handing
// a message to a Provider.
type PhoneOptOutStore interface {
	// Upsert is idempotent on (company_id, phone). matchReason and
	// receivedVia are stored verbatim.
	Upsert(ctx context.Context, companyID uuid.UUID, phone, matchReason, receivedVia, rawBody string) error

	// IsOptedOut reports whether the phone has an opt-out row in the
	// company. Used by contact-list generation and by marketing send
	// pre-flight checks.
	IsOptedOut(ctx context.Context, companyID uuid.UUID, phone string) (bool, error)

	// Remove clears any opt-out row for phone within companyID — the
	// START / UNSTOP (opt back in) path. Idempotent: removing a phone with
	// no existing opt-out row succeeds silently rather than erroring.
	//
	// This method has NO equivalent in the politihub source this file was
	// ported from. It was added reconciling against
	// navigators-go/internal/navigators/suppression_service.go's
	// RemoveFromSuppressionList + sms_compliance.go's ProcessOptOut STOP/
	// START handling, which politihub's phone_optout_store.go does not
	// cover (politihub has no START path at all). See this objective's
	// 40-05-SUMMARY.md for the full reconciliation.
	Remove(ctx context.Context, companyID uuid.UUID, phone string) error
}

// TCPAService composes the three persistence dependencies and runs the
// fuzzy matcher across them.
type TCPAService struct {
	recipients RecipientLookup
	consents   ConsentRecorder
	phoneStore PhoneOptOutStore
}

// NewTCPAService constructs a TCPAService. All dependencies are optional —
// a nil dependency yields a service that no-ops the corresponding side
// effect (used in tests, and by callers that haven't wired a recipient
// concept at all).
func NewTCPAService(recipients RecipientLookup, consents ConsentRecorder, phoneStore PhoneOptOutStore) *TCPAService {
	return &TCPAService{recipients: recipients, consents: consents, phoneStore: phoneStore}
}

// HandleInboundBody runs the TCPA matcher on a verified inbound SMS body and
// records opt-outs (STOP) or clears them (START) against both the phone
// store (always) and, for STOP, the consent recorder (when the phone is
// linked to a recipient).
//
// Returns (matched=true, nil) when the body was recognized as a STOP or
// START keyword event, so the caller can short-circuit the rest of inbound
// processing. Returns (false, nil) for ordinary message bodies. HELP is
// NOT handled here — FuzzyMatchHelp is called independently by the webhook
// layer to build an auto-reply, since a HELP reply has no persistence side
// effect of its own.
//
// Storage failures are returned as errors — the caller decides whether to
// surface them. Webhook handlers typically log and still return 200 to the
// provider so the message is not retried.
func (s *TCPAService) HandleInboundBody(ctx context.Context, companyID uuid.UUID, providerType ProviderType, msg InboundSMS) (matched bool, err error) {
	if stopped, reason := FuzzyMatchStop(msg.Body); stopped {
		return s.handleStop(ctx, companyID, providerType, msg, reason)
	}
	if started, _ := FuzzyMatchStart(msg.Body); started {
		return s.handleStart(ctx, companyID, msg)
	}
	return false, nil
}

// handleStop always upserts the phone-keyed opt-out (the gate marketing
// pre-flight checks consult before sending), and additionally records a
// consent-ledger opt-out when the phone is linked to a recipient.
func (s *TCPAService) handleStop(ctx context.Context, companyID uuid.UUID, providerType ProviderType, msg InboundSMS, reason MatchReason) (bool, error) {
	if s.phoneStore != nil {
		if err := s.phoneStore.Upsert(ctx, companyID, msg.From, string(reason), string(providerType), msg.Body); err != nil {
			return true, err
		}
	}

	if s.recipients != nil && s.consents != nil {
		recipientID, found, err := s.recipients.FindRecipientByPhone(ctx, companyID, msg.From)
		if err != nil {
			return true, err
		}
		if found {
			if err := s.consents.RecordOptOut(ctx, companyID, recipientID, "sms", string(providerType)); err != nil {
				return true, err
			}
		}
	}

	return true, nil
}

// handleStart clears the phone-keyed opt-out row, if any. There is
// deliberately no consent-ledger counterpart call here: ConsentRecorder's
// shape (RecordOptOut only) carries over from politihub unchanged, and this
// TRD's scope is the phone-store opt-back-in path navigators covers, not
// widening the consent-ledger interface.
func (s *TCPAService) handleStart(ctx context.Context, companyID uuid.UUID, msg InboundSMS) (bool, error) {
	if s.phoneStore != nil {
		if err := s.phoneStore.Remove(ctx, companyID, msg.From); err != nil {
			return true, err
		}
	}
	return true, nil
}

// CheckSendAllowed is the precondition gate an outbound send call site MUST
// run BEFORE handing anything to a Provider. It is deliberately a
// precondition check, not a post-send or post-queue filter: a caller that
// queues a send and filters opted-out numbers afterward has already leaked
// the message into a queue this package does not control.
//
// FAILS CLOSED: an error from the underlying store (unknown state) is
// treated as "not allowed," mirroring
// navigators-go/internal/navigators/suppression_service.go's
// IsVoterSuppressed fail-closed posture — an outage in the opt-out store
// must never look like consent.
//
// Reconciled against navigators-go/internal/navigators/sms_compliance.go's
// CheckSendAllowed, which additionally gates on quiet hours. Quiet-hours
// enforcement is deliberately NOT ported here: it is a per-tenant scheduling
// policy, not a TCPA opt-out concern, and this TRD's file_ownership is
// scoped to tcpa.go / optout_store.go. A future TRD can compose a
// quiet-hours check alongside this one without this package needing to know
// about it.
func (s *TCPAService) CheckSendAllowed(ctx context.Context, companyID uuid.UUID, phone string) error {
	if s.phoneStore == nil {
		return nil
	}
	optedOut, err := s.phoneStore.IsOptedOut(ctx, companyID, phone)
	if err != nil {
		return fmt.Errorf("telephony tcpa: opt-out check failed, failing closed: %w", err)
	}
	if optedOut {
		return ErrRecipientOptedOut
	}
	return nil
}
