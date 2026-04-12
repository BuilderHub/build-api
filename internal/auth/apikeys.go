package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/builderhub/build-api/internal/db"
	"github.com/jackc/pgx/v5"
)

// HashAPIKeyToken returns the SHA-256 hex digest of the full secret (what we store in user_api_keys.key_hash).
func HashAPIKeyToken(fullToken string) string {
	sum := sha256.Sum256([]byte(fullToken))
	return hex.EncodeToString(sum[:])
}

// GenerateUserAPIKey returns a new secret of the form bh_<base64url> and a short prefix for display.
func GenerateUserAPIKey() (full string, prefix string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("random: %w", err)
	}
	enc := base64.RawURLEncoding.EncodeToString(b)
	full = "bh_" + enc
	prefix = full
	if len(full) > 12 {
		prefix = full[:12]
	}
	return full, prefix, nil
}

// LookupAPIKey resolves a bearer secret to user id and scopes; updates last_used_at when found.
func LookupAPIKey(ctx context.Context, pool *db.Pool, fullToken string) (userID string, scopes []string, err error) {
	if fullToken == "" {
		return "", nil, fmt.Errorf("empty token")
	}
	hash := HashAPIKeyToken(fullToken)
	var keyID string
	err = pool.QueryRow(ctx,
		`SELECT id, user_id, scopes FROM user_api_keys
		 WHERE key_hash = $1 AND (expires_at IS NULL OR expires_at > NOW())`,
		hash,
	).Scan(&keyID, &userID, &scopes)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil, fmt.Errorf("not found")
		}
		return "", nil, err
	}
	_, _ = pool.Exec(ctx, `UPDATE user_api_keys SET last_used_at = NOW() WHERE id = $1`, keyID)
	return userID, scopes, nil
}
