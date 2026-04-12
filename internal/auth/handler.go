package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	buildapiv1 "github.com/builderhub/build-api/api/gen/buildapi/v1"
	"github.com/builderhub/build-api/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuthService struct {
	buildapiv1.UnimplementedAuthServiceServer
	pool *db.Pool
	jwt  *JWTManager
	log  *zap.SugaredLogger
}

func NewAuthService(pool *db.Pool, jwt *JWTManager, log *zap.SugaredLogger) *AuthService {
	return &AuthService{pool: pool, jwt: jwt, log: log}
}

func (s *AuthService) Register(ctx context.Context, req *buildapiv1.RegisterRequest) (*buildapiv1.RegisterResponse, error) {
	email := strings.TrimSpace(req.Email)
	name := strings.TrimSpace(req.Name)
	if email == "" || req.Password == "" || name == "" {
		return nil, status.Error(codes.InvalidArgument, "email, password, and name are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "hash password: %v", err)
	}
	var userID string
	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES ($1, $2, $3) RETURNING id`,
		email, string(hash), name,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		if isUniqueViolation(err) {
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		return nil, status.Errorf(codes.Internal, "insert user: %v", err)
	}
	var createdAt int64
	err = s.pool.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM created_at)::bigint FROM users WHERE id = $1`, userID).Scan(&createdAt)
	if err != nil {
		createdAt = 0
	}
	user := &buildapiv1.User{Id: userID, Email: email, Name: name, CreatedAt: createdAt}
	accessToken, expiresIn, _ := s.jwt.CreateAccessToken(userID, email)
	refreshToken, _ := s.jwt.CreateRefreshToken(userID, email)
	s.log.Infow("user registered", "email", email, "id", userID)
	return &buildapiv1.RegisterResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
	}, nil
}

func (s *AuthService) Login(ctx context.Context, req *buildapiv1.LoginRequest) (*buildapiv1.LoginResponse, error) {
	if req.Email == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "email and password are required")
	}
	var userID, name, hash string
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, password_hash FROM users WHERE email = $1`,
		req.Email,
	).Scan(&userID, &name, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.Unauthenticated, "invalid email or password")
		}
		return nil, status.Errorf(codes.Internal, "query user: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid email or password")
	}
	var createdAt int64
	_ = s.pool.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM created_at)::bigint FROM users WHERE id = $1`, userID).Scan(&createdAt)
	user := &buildapiv1.User{Id: userID, Email: req.Email, Name: name, CreatedAt: createdAt}
	accessToken, expiresIn, _ := s.jwt.CreateAccessToken(userID, req.Email)
	refreshToken, _ := s.jwt.CreateRefreshToken(userID, req.Email)
	s.log.Infow("user logged in", "email", req.Email)
	return &buildapiv1.LoginResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
	}, nil
}

func (s *AuthService) GetMe(ctx context.Context, req *buildapiv1.GetMeRequest) (*buildapiv1.GetMeResponse, error) {
	userID := UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	var email, name string
	var createdAt int64
	err := s.pool.QueryRow(ctx,
		`SELECT email, name, EXTRACT(EPOCH FROM created_at)::bigint FROM users WHERE id = $1`,
		userID,
	).Scan(&email, &name, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Errorf(codes.Internal, "query user: %v", err)
	}
	return &buildapiv1.GetMeResponse{
		User: &buildapiv1.User{Id: userID, Email: email, Name: name, CreatedAt: createdAt},
	}, nil
}

func (s *AuthService) UpdateProfile(ctx context.Context, req *buildapiv1.UpdateProfileRequest) (*buildapiv1.UpdateProfileResponse, error) {
	userID := UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE users SET name = $1 WHERE id = $2`, name, userID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update profile: %v", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	var email string
	var createdAt int64
	err = s.pool.QueryRow(ctx,
		`SELECT email, EXTRACT(EPOCH FROM created_at)::bigint FROM users WHERE id = $1`,
		userID,
	).Scan(&email, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Errorf(codes.Internal, "query user: %v", err)
	}
	return &buildapiv1.UpdateProfileResponse{
		User: &buildapiv1.User{Id: userID, Email: email, Name: name, CreatedAt: createdAt},
	}, nil
}

func (s *AuthService) RefreshToken(ctx context.Context, req *buildapiv1.RefreshTokenRequest) (*buildapiv1.RefreshTokenResponse, error) {
	if req.RefreshToken == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}
	claims, err := s.jwt.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	accessToken, expiresIn, err := s.jwt.CreateAccessToken(claims.UserID, claims.Email)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create token: %v", err)
	}
	return &buildapiv1.RefreshTokenResponse{
		AccessToken: accessToken,
		ExpiresIn:   expiresIn,
	}, nil
}

func (s *AuthService) CreateUserApiKey(ctx context.Context, req *buildapiv1.CreateUserApiKeyRequest) (*buildapiv1.CreateUserApiKeyResponse, error) {
	userID := UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	scopes := req.GetScopes()
	if len(scopes) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one scope is required")
	}
	seen := make(map[string]struct{})
	for _, sc := range scopes {
		sc = strings.TrimSpace(sc)
		if sc == "" {
			return nil, status.Error(codes.InvalidArgument, "invalid empty scope")
		}
		if _, ok := ValidScopes[sc]; !ok {
			return nil, status.Error(codes.InvalidArgument, "unknown scope: "+sc)
		}
		if _, dup := seen[sc]; dup {
			return nil, status.Error(codes.InvalidArgument, "duplicate scope: "+sc)
		}
		seen[sc] = struct{}{}
	}
	var expiresAtValue interface{}
	switch req.GetExpiresInDays() {
	case -1:
		expiresAtValue = nil
	case 0:
		expiresAtValue = time.Now().UTC().AddDate(0, 0, 365)
	default:
		d := int(req.GetExpiresInDays())
		if d < 1 || d > 3650 {
			return nil, status.Error(codes.InvalidArgument, "expires_in_days must be -1 (never), 0 (default 365 days), or between 1 and 3650")
		}
		expiresAtValue = time.Now().UTC().AddDate(0, 0, d)
	}

	full, keyPrefix, err := GenerateUserAPIKey()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate api key: %v", err)
	}
	hash := HashAPIKeyToken(full)
	var id string
	var createdAt int64
	var expiresEpoch sql.NullInt64
	err = s.pool.QueryRow(ctx,
		`INSERT INTO user_api_keys (user_id, name, key_prefix, key_hash, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, EXTRACT(EPOCH FROM created_at)::bigint, EXTRACT(EPOCH FROM expires_at)::bigint`,
		userID, name, keyPrefix, hash, scopes, expiresAtValue,
	).Scan(&id, &createdAt, &expiresEpoch)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create api key: %v", err)
	}
	var expiresAtProto int64
	if expiresEpoch.Valid {
		expiresAtProto = expiresEpoch.Int64
	}
	s.log.Infow("user api key created", "user_id", userID, "key_id", id)
	return &buildapiv1.CreateUserApiKeyResponse{
		Key: &buildapiv1.UserApiKeyMetadata{
			Id:         id,
			Name:       name,
			KeyPrefix:  keyPrefix,
			Scopes:     scopes,
			CreatedAt:  createdAt,
			LastUsedAt: 0,
			ExpiresAt:  expiresAtProto,
		},
		Token: full,
	}, nil
}

func (s *AuthService) ListUserApiKeys(ctx context.Context, _ *buildapiv1.ListUserApiKeysRequest) (*buildapiv1.ListUserApiKeysResponse, error) {
	userID := UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, key_prefix, scopes,
			EXTRACT(EPOCH FROM created_at)::bigint,
			EXTRACT(EPOCH FROM last_used_at)::bigint,
			EXTRACT(EPOCH FROM expires_at)::bigint
		 FROM user_api_keys WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list api keys: %v", err)
	}
	defer rows.Close()
	var keys []*buildapiv1.UserApiKeyMetadata
	for rows.Next() {
		var id, name, prefix string
		var scopes []string
		var createdAt int64
		var lastUsed, expiresAt sql.NullInt64
		if err := rows.Scan(&id, &name, &prefix, &scopes, &createdAt, &lastUsed, &expiresAt); err != nil {
			return nil, status.Errorf(codes.Internal, "scan api key: %v", err)
		}
		meta := &buildapiv1.UserApiKeyMetadata{
			Id:        id,
			Name:      name,
			KeyPrefix: prefix,
			Scopes:    scopes,
			CreatedAt: createdAt,
		}
		if expiresAt.Valid {
			meta.ExpiresAt = expiresAt.Int64
		}
		if lastUsed.Valid {
			meta.LastUsedAt = lastUsed.Int64
		}
		keys = append(keys, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "list api keys: %v", err)
	}
	return &buildapiv1.ListUserApiKeysResponse{Keys: keys}, nil
}

func (s *AuthService) RevokeUserApiKey(ctx context.Context, req *buildapiv1.RevokeUserApiKeyRequest) (*buildapiv1.RevokeUserApiKeyResponse, error) {
	userID := UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	id := strings.TrimSpace(req.GetId())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM user_api_keys WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "revoke api key: %v", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, status.Error(codes.NotFound, "api key not found")
	}
	s.log.Infow("user api key revoked", "user_id", userID, "key_id", id)
	return &buildapiv1.RevokeUserApiKeyResponse{}, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" // unique_violation
	}
	return strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint")
}
