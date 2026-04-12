UPDATE user_api_keys
SET expires_at = created_at + interval '36500 days'
WHERE expires_at IS NULL;
ALTER TABLE user_api_keys ALTER COLUMN expires_at SET NOT NULL;
