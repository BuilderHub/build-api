-- API key expiry (reject auth after this instant)
ALTER TABLE user_api_keys ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
UPDATE user_api_keys SET expires_at = created_at + interval '365 days' WHERE expires_at IS NULL;
ALTER TABLE user_api_keys ALTER COLUMN expires_at SET NOT NULL;
