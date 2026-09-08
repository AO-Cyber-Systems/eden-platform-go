-- Phone-keyed TCPA opt-outs (no linked recipient required). This is the
-- durable, provider-neutral record TCPAService.CheckSendAllowed and
-- PhoneOptOutStore.IsOptedOut consult BEFORE an outbound send -- a
-- precondition, not a post-send filter (see platform/telephony/tcpa.go).
--
-- Deliberately NOT created alongside migration 016: TRD 40-03's own scope
-- was the tenant config table only, and this table's shape depends on a
-- normalization design decision (see below) that belongs with the Go store
-- that owns it (platform/telephony/optout_store.go, TRD 40-05).
--
-- phone_normalized is populated and compared Go-side, by optout_store.go's
-- normalizePhone, not by a Postgres function. politihub's equivalent table
-- depended on a custom SQL function (politihub_normalize_phone) shipped in
-- a separate migration; that function and its Go callers drifted out of
-- sync in production (see optout_store.go's header comment for the
-- measured before/after). Normalizing once, in Go, on both the write path
-- (Upsert, Remove) and the read path (IsOptedOut) removes the possibility
-- of a second, disagreeing definition of "equivalent phone number".
CREATE TABLE IF NOT EXISTS telephony_phone_optouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    phone TEXT NOT NULL,
    phone_normalized TEXT NOT NULL,
    match_reason TEXT NOT NULL,
    received_via TEXT NOT NULL,
    raw_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A phone opts out AT MOST ONCE per company. Upsert (STOP) writes through
-- this index; Remove (START) deletes the row it names, so a subsequent
-- STOP can insert cleanly again -- this is the ordering the revocation
-- must-have exercises (STOP -> START -> STOP).
CREATE UNIQUE INDEX uq_telephony_phone_optouts_company_phone
    ON telephony_phone_optouts (company_id, phone_normalized);
