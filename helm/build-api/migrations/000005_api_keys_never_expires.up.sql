-- Allow keys that never expire (NULL = no expiry)
ALTER TABLE user_api_keys ALTER COLUMN expires_at DROP NOT NULL;
