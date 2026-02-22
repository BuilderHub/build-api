package auth

import (
	"context"
	"errors"
	"strings"

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
	if req.Email == "" || req.Password == "" || req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "email, password, and name are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "hash password: %v", err)
	}
	var userID string
	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES ($1, $2, $3) RETURNING id`,
		req.Email, string(hash), req.Name,
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
	user := &buildapiv1.User{Id: userID, Email: req.Email, Name: req.Name, CreatedAt: createdAt}
	accessToken, expiresIn, _ := s.jwt.CreateAccessToken(userID, req.Email)
	refreshToken, _ := s.jwt.CreateRefreshToken(userID, req.Email)
	s.log.Infow("user registered", "email", req.Email, "id", userID)
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" // unique_violation
	}
	return strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint")
}
