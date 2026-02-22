package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	AccessTokenExpiry  = 15 * time.Minute
	RefreshTokenExpiry = 7 * 24 * time.Hour
	tokenTypeAccess    = "access"
	tokenTypeRefresh   = "refresh"
)

type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Type   string `json:"type"` // access or refresh
}

func NewJWTManager(secret string) *JWTManager {
	return &JWTManager{secret: []byte(secret)}
}

type JWTManager struct {
	secret []byte
}

func (m *JWTManager) CreateAccessToken(userID, email string) (string, int64, error) {
	exp := time.Now().Add(AccessTokenExpiry)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		UserID: userID,
		Email:  email,
		Type:   tokenTypeAccess,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", 0, err
	}
	return signed, int64(AccessTokenExpiry.Seconds()), nil
}

func (m *JWTManager) CreateRefreshToken(userID, email string) (string, error) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(RefreshTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		UserID: userID,
		Email:  email,
		Type:   tokenTypeRefresh,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

func (m *JWTManager) ValidateAccessToken(tokenStr string) (*Claims, error) {
	claims, err := m.parseToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Type != tokenTypeAccess {
		return nil, errors.New("invalid token type")
	}
	return claims, nil
}

func (m *JWTManager) ValidateRefreshToken(tokenStr string) (*Claims, error) {
	claims, err := m.parseToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Type != tokenTypeRefresh {
		return nil, errors.New("invalid token type")
	}
	return claims, nil
}

func (m *JWTManager) parseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
