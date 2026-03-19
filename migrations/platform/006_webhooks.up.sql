-- Webhooks
CREATE TABLE IF NOT EXISTS webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    secret TEXT NOT NULL,
    events TEXT[] NOT NULL DEFAULT '{}',
    active BOOLEAN NOT NULL DEFAULT true,
    consecutive_fails INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhooks_company ON webhooks (company_id);
CREATE INDEX idx_webhooks_active ON webhooks (company_id, active) WHERE active = true;

-- Webhook deliveries
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id UUID NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'success', 'failed', 'exhausted')),
    status_code INTEGER NOT NULL DEFAULT 0,
    response_body TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_deliveries_webhook ON webhook_deliveries (webhook_id, created_at DESC);
CREATE INDEX idx_webhook_deliveries_pending ON webhook_deliveries (status, next_retry_at)
    WHERE status IN ('pending', 'failed');
