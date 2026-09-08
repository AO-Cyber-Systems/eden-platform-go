package telephony

// optout_store.go is PhoneOptOutStore's production Postgres implementation,
// backed by the telephony_phone_optouts table (migration 017).
//
// Ported from politihub/go/internal/telephony/phone_optout_store.go
// (Objective 40 TRD 40-05), with one deliberate design change: phone
// normalization is Go-side, not a Postgres function.
//
// WHY. politihub's IsOptedOut compared against politihub_normalize_phone, a
// custom SQL function shipped in a SEPARATE migration (067) from the table
// itself. In production, Upsert stored the provider's `From` verbatim while
// at least one caller queried with a differently-formatted string the
// import pipeline had produced, and the mismatch silently let an
// already-opted-out number through — measured against the same fixture: 0
// rows matched with "(207) 555-0142", 1 row matched with "+12075550142"
// (see politihub's phone_optout_store.go:45-52). That bug was a consequence
// of "equivalent phone number" being defined in two places — a Go import
// path and a SQL function — that could drift out of sync.
//
// This store normalizes ONCE, in Go, against the SAME function
// (normalizePhone below) on both the write path (Upsert, Remove) and the
// read path (IsOptedOut). The normalized form is persisted in
// phone_normalized and is what the unique index and every lookup key on —
// there is no second definition to drift out of sync with.
import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// normalizePhone strips everything except digits and a leading '+', then
// assumes NANP (prefixes "+1") for a bare 10-digit number. This is a
// best-effort canonicalization for opt-out matching, not a validator —
// garbage input normalizes to whatever digits it contains, which still
// compares correctly against another equally-garbage write of the SAME
// number (idempotent matching), even though it may not compare correctly
// against a well-formed E.164 write of a genuinely different representation
// of the same number outside NANP. Both Twilio and SignalWire deliver
// InboundSMS.From in E.164 already (see models.go), so in practice this
// mainly defends an operator-entered support override, not the hot path.
func normalizePhone(phone string) string {
	var b strings.Builder
	for i, r := range phone {
		if r == '+' && i == 0 {
			b.WriteRune(r)
			continue
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if !strings.HasPrefix(digits, "+") && len(digits) == 10 {
		return "+1" + digits
	}
	return digits
}

// PostgresPhoneOptOutStore is the production PhoneOptOutStore.
type PostgresPhoneOptOutStore struct {
	pool *pgxpool.Pool
}

// NewPostgresPhoneOptOutStore constructs a PostgresPhoneOptOutStore.
func NewPostgresPhoneOptOutStore(pool *pgxpool.Pool) *PostgresPhoneOptOutStore {
	return &PostgresPhoneOptOutStore{pool: pool}
}

// Upsert implements PhoneOptOutStore. Idempotent on
// (company_id, phone_normalized) — a phone that opts out twice (e.g. two
// different STOP spellings) updates the same row rather than erroring or
// duplicating.
func (s *PostgresPhoneOptOutStore) Upsert(ctx context.Context, companyID uuid.UUID, phone, matchReason, receivedVia, rawBody string) error {
	if phone == "" {
		return nil
	}
	normalized := normalizePhone(phone)
	if normalized == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO telephony_phone_optouts
		    (company_id, phone, phone_normalized, match_reason, received_via, raw_body)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
		ON CONFLICT (company_id, phone_normalized) DO UPDATE
		    SET phone        = EXCLUDED.phone,
		        match_reason = EXCLUDED.match_reason,
		        received_via = EXCLUDED.received_via,
		        raw_body     = EXCLUDED.raw_body,
		        updated_at   = now()`,
		companyID, phone, normalized, matchReason, receivedVia, rawBody)
	if err != nil {
		return fmt.Errorf("telephony phone optout upsert: %w", err)
	}
	return nil
}

// IsOptedOut implements PhoneOptOutStore. This is the precondition check
// TCPAService.CheckSendAllowed runs BEFORE a caller may hand a message to a
// Provider.
func (s *PostgresPhoneOptOutStore) IsOptedOut(ctx context.Context, companyID uuid.UUID, phone string) (bool, error) {
	normalized := normalizePhone(phone)
	if normalized == "" {
		return false, nil
	}
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT 1 FROM telephony_phone_optouts
		 WHERE company_id = $1 AND phone_normalized = $2
		 LIMIT 1`, companyID, normalized).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("telephony phone optout check: %w", err)
	}
	return true, nil
}

// Remove implements PhoneOptOutStore — the START / UNSTOP (opt back in)
// path. Idempotent: removing a phone with no existing opt-out row succeeds
// silently rather than erroring, matching Upsert's idempotent posture on
// the opposite transition.
//
// This method, and the table row it deletes, have no equivalent in the
// politihub source this file was ported from — politihub's phone_optout_store.go
// has no opt-back-in path at all. Added reconciling against
// navigators-go/internal/navigators/suppression_service.go's
// RemoveFromSuppressionList and sms_compliance.go's ProcessOptOut STOP/
// START handling. See this objective's 40-05-SUMMARY.md for the full
// reconciliation record.
func (s *PostgresPhoneOptOutStore) Remove(ctx context.Context, companyID uuid.UUID, phone string) error {
	normalized := normalizePhone(phone)
	if normalized == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		DELETE FROM telephony_phone_optouts
		 WHERE company_id = $1 AND phone_normalized = $2`, companyID, normalized)
	if err != nil {
		return fmt.Errorf("telephony phone optout remove: %w", err)
	}
	return nil
}
