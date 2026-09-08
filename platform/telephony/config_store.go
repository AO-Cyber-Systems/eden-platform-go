package telephony

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSendingNumberClaimed is returned by ConfigStore.Upsert when the
// requested sending_number is already owned by ANOTHER company's active
// configuration for the same provider. A receiving number must resolve to
// AT MOST ONE tenant: LookupBySendingNumber is the inbound-webhook
// tenant-resolution chokepoint (a status callback or inbound SMS carries
// only the destination number in its body, never a tenant identifier), so a
// shared active number would route inbound traffic to a non-deterministic
// tenant and verify its HMAC signature against the wrong secret. Reconciled
// against eden-biz/telephonycreds' ErrTelephonyNumberClaimed, which enforces
// the identical invariant for its single-row-per-company model -- this
// fails closed the same way, mapped from the same class of Postgres unique
// violation (see migration 016's partial unique index).
var ErrSendingNumberClaimed = errors.New("telephony: sending number already claimed by another tenant")

// ConfigStore persists tenant_telephony_config rows. Implementations are
// responsible for encrypting auth_token and webhook_secret at rest.
type ConfigStore interface {
	// Get returns the active config for a company. ErrTenantNotConfigured
	// when no active row exists.
	Get(ctx context.Context, companyID uuid.UUID) (Config, error)

	// LookupBySendingNumber finds the active config that owns the given
	// destination number. Used by webhook handlers to resolve which tenant
	// a status callback / inbound SMS belongs to from the form's `To` field
	// -- inbound routing depends on it.
	LookupBySendingNumber(ctx context.Context, provider ProviderType, number string) (Config, error)

	// Upsert writes a new active row, deactivating any prior active row for
	// the same company. Plaintext auth_token and webhook_secret are
	// encrypted by the implementation. Returns ErrSendingNumberClaimed when
	// another company already actively owns c.SendingNumber for c.Provider.
	Upsert(ctx context.Context, c Config) error

	// Deactivate clears the active flag on the company's current row.
	Deactivate(ctx context.Context, companyID uuid.UUID) error

	// List returns all active rows. Used by platform-admin screens.
	List(ctx context.Context) ([]Config, error)
}

// Encrypter is the at-rest envelope used for auth_token and webhook_secret.
// The interface is satisfied by NopEncrypter (dev/test) or FieldEncrypter,
// the production adapter over platform/encryption.FieldEncryptor.
type Encrypter interface {
	Encrypt(plaintext string) ([]byte, error)
	Decrypt(ciphertext []byte) (string, error)
}

// NopEncrypter passes plaintext through unchanged. This is the default for
// local dev and for tests that don't need to exercise the encryption seam
// itself.
type NopEncrypter struct{}

// Encrypt implements Encrypter.
func (NopEncrypter) Encrypt(plaintext string) ([]byte, error) {
	return []byte(plaintext), nil
}

// Decrypt implements Encrypter.
func (NopEncrypter) Decrypt(ciphertext []byte) (string, error) {
	return string(ciphertext), nil
}

// PostgresConfigStore is the production ConfigStore, backed by the
// tenant_telephony_config table (migration 016).
type PostgresConfigStore struct {
	pool *pgxpool.Pool
	enc  Encrypter
}

// NewPostgresConfigStore constructs a PostgresConfigStore. enc may be nil --
// NopEncrypter is used in that case. Production callers should pass a
// *FieldEncrypter constructed from the platform data key.
func NewPostgresConfigStore(pool *pgxpool.Pool, enc Encrypter) *PostgresConfigStore {
	if enc == nil {
		enc = NopEncrypter{}
	}
	return &PostgresConfigStore{pool: pool, enc: enc}
}

// Get implements ConfigStore.
func (s *PostgresConfigStore) Get(ctx context.Context, companyID uuid.UUID) (Config, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT company_id, provider, account_sid,
		       auth_token_encrypted, webhook_secret_encrypted,
		       COALESCE(space_url, ''), sending_number, is_active
		  FROM tenant_telephony_config
		 WHERE company_id = $1 AND is_active = true`, companyID)

	c, authEnc, secretEnc, err := scanConfig(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Config{}, ErrTenantNotConfigured
	}
	if err != nil {
		return Config{}, fmt.Errorf("telephony config get: %w", err)
	}
	if err := s.decryptInto(&c, authEnc, secretEnc); err != nil {
		return Config{}, err
	}
	return c, nil
}

// LookupBySendingNumber implements ConfigStore.
func (s *PostgresConfigStore) LookupBySendingNumber(ctx context.Context, provider ProviderType, number string) (Config, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT company_id, provider, account_sid,
		       auth_token_encrypted, webhook_secret_encrypted,
		       COALESCE(space_url, ''), sending_number, is_active
		  FROM tenant_telephony_config
		 WHERE provider = $1 AND sending_number = $2 AND is_active = true
		 LIMIT 1`, string(provider), number)

	c, authEnc, secretEnc, err := scanConfig(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Config{}, ErrTenantNotConfigured
	}
	if err != nil {
		return Config{}, fmt.Errorf("telephony config lookup: %w", err)
	}
	if err := s.decryptInto(&c, authEnc, secretEnc); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Upsert implements ConfigStore.
func (s *PostgresConfigStore) Upsert(ctx context.Context, c Config) error {
	authEnc, err := s.enc.Encrypt(c.AuthToken)
	if err != nil {
		return fmt.Errorf("telephony config encrypt auth: %w", err)
	}
	var secretEnc []byte
	if c.WebhookSecret != "" {
		secretEnc, err = s.enc.Encrypt(c.WebhookSecret)
		if err != nil {
			return fmt.Errorf("telephony config encrypt webhook secret: %w", err)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("telephony config begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Deactivate any prior active row for this company. This is a no-op for
	// a first-time Upsert.
	if _, err := tx.Exec(ctx, `
		UPDATE tenant_telephony_config
		   SET is_active = false
		 WHERE company_id = $1 AND is_active = true`, c.CompanyID); err != nil {
		return fmt.Errorf("telephony config deactivate prior: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant_telephony_config
		    (company_id, provider, account_sid,
		     auth_token_encrypted, webhook_secret_encrypted,
		     space_url, sending_number, is_active)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, true)`,
		c.CompanyID, string(c.Provider), c.AccountSID,
		authEnc, secretEnc, c.SpaceURL, c.SendingNumber); err != nil {
		// migration 016's uq_tenant_telephony_config_active_number is the ONLY
		// unique constraint an insert here can hit -- the deactivate above
		// already cleared this company's own prior active row, so any 23505
		// that escapes means ANOTHER company's active row already owns this
		// (provider, sending_number) pair. Map it to a typed conflict (never a
		// raw 500) rather than silently accepting an ambiguous inbound route.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrSendingNumberClaimed
		}
		return fmt.Errorf("telephony config insert: %w", err)
	}
	return tx.Commit(ctx)
}

// Deactivate implements ConfigStore.
func (s *PostgresConfigStore) Deactivate(ctx context.Context, companyID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE tenant_telephony_config
		   SET is_active = false
		 WHERE company_id = $1 AND is_active = true`, companyID)
	if err != nil {
		return fmt.Errorf("telephony config deactivate: %w", err)
	}
	return nil
}

// List implements ConfigStore.
func (s *PostgresConfigStore) List(ctx context.Context) ([]Config, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT company_id, provider, account_sid,
		       auth_token_encrypted, webhook_secret_encrypted,
		       COALESCE(space_url, ''), sending_number, is_active
		  FROM tenant_telephony_config
		 WHERE is_active = true
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("telephony config list: %w", err)
	}
	defer rows.Close()

	out := []Config{}
	for rows.Next() {
		c, _, _, err := scanConfig(rows)
		if err != nil {
			return nil, fmt.Errorf("telephony config list scan: %w", err)
		}
		// Don't decrypt secrets in List -- admin UI uses masked display only.
		// Callers that need plaintext must call Get individually. c.AuthToken
		// and c.WebhookSecret are left at their zero value here.
		out = append(out, c)
	}
	return out, rows.Err()
}

// rowScanner is the subset of pgx.Row / pgx.Rows that scanConfig needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanConfig scans the eight columns shared by Get, LookupBySendingNumber,
// and List into a Config plus the two still-encrypted byte slices. AuthToken
// and WebhookSecret on the returned Config are left at their zero value --
// decryptInto (or the List caller, which skips decryption entirely) is
// responsible for populating them.
func scanConfig(row rowScanner) (c Config, authEnc, secretEnc []byte, err error) {
	var providerStr string
	if err := row.Scan(
		&c.CompanyID, &providerStr, &c.AccountSID,
		&authEnc, &secretEnc, &c.SpaceURL, &c.SendingNumber, &c.IsActive,
	); err != nil {
		return Config{}, nil, nil, err
	}
	c.Provider = ProviderType(providerStr)
	return c, authEnc, secretEnc, nil
}

// decryptInto populates c.AuthToken / c.WebhookSecret by decrypting the raw
// bytes scanConfig returned, using the store's configured Encrypter.
func (s *PostgresConfigStore) decryptInto(c *Config, authEnc, secretEnc []byte) error {
	plain, err := s.enc.Decrypt(authEnc)
	if err != nil {
		return fmt.Errorf("telephony config decrypt auth: %w", err)
	}
	c.AuthToken = plain
	if len(secretEnc) > 0 {
		secret, err := s.enc.Decrypt(secretEnc)
		if err != nil {
			return fmt.Errorf("telephony config decrypt webhook secret: %w", err)
		}
		c.WebhookSecret = secret
	}
	return nil
}
