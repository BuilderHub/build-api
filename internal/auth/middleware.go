package auth

import (
	"context"
	"strings"

	"github.com/builderhub/build-api/internal/db"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AuthLogger logs auth failures. Required when creating interceptors.
type AuthLogger interface {
	Warnf(format string, args ...interface{})
}

// PublicMethods are RPC full names that do not require authentication.
var PublicMethods = map[string]bool{
	"/buildapi.v1.AuthService/Register":    true,
	"/buildapi.v1.AuthService/Login":        true,
	"/buildapi.v1.AuthService/RefreshToken": true,
	"/buildapi.v1.BuildAPI/HealthCheck":    true,
}

func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

func enforceAccess(fullMethod string, p *Principal) error {
	if JWTOnlyMethods[fullMethod] {
		if !p.FullAccess {
			return status.Error(codes.PermissionDenied, "this operation requires a browser session")
		}
		return nil
	}
	if req, ok := MethodRequiredScope[fullMethod]; ok {
		if p.FullAccess {
			return nil
		}
		if hasScope(p.Scopes, req) {
			return nil
		}
		return status.Error(codes.PermissionDenied, "insufficient API key scope")
	}
	if p.FullAccess {
		return nil
	}
	return status.Error(codes.PermissionDenied, "insufficient API key scope")
}

// UnaryServerInterceptor validates JWT or API keys, attaches identity, and enforces scopes.
func UnaryServerInterceptor(jwt *JWTManager, pool *db.Pool, log AuthLogger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if PublicMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		token, err := extractBearerToken(ctx, info.FullMethod, log)
		if err != nil {
			return nil, err
		}
		p, err := authenticate(ctx, pool, jwt, token, log, info.FullMethod)
		if err != nil {
			return nil, err
		}
		ctx = WithPrincipal(ctx, p)
		if err := enforceAccess(info.FullMethod, p); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor validates JWT or API keys for streaming RPCs.
func StreamServerInterceptor(jwt *JWTManager, pool *db.Pool, log AuthLogger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if PublicMethods[info.FullMethod] {
			return handler(srv, ss)
		}
		ctx := ss.Context()
		token, err := extractBearerToken(ctx, info.FullMethod, log)
		if err != nil {
			return err
		}
		p, err := authenticate(ctx, pool, jwt, token, log, info.FullMethod)
		if err != nil {
			return err
		}
		ctx = WithPrincipal(ctx, p)
		if err := enforceAccess(info.FullMethod, p); err != nil {
			return err
		}
		return handler(srv, &streamWithContext{ServerStream: ss, ctx: ctx})
	}
}

func authenticate(ctx context.Context, pool *db.Pool, jwt *JWTManager, token string, log AuthLogger, method string) (*Principal, error) {
	if looksLikeJWT(token) {
		claims, err := jwt.ValidateAccessToken(token)
		if err != nil {
			log.Warnf("auth failed method=%s reason=jwt_validation err=%v", method, err)
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		return &Principal{UserID: claims.UserID, FullAccess: true}, nil
	}
	if !strings.HasPrefix(token, "bh_") {
		log.Warnf("auth failed method=%s reason=unknown_token_shape", method)
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
	}
	userID, scopes, err := LookupAPIKey(ctx, pool, token)
	if err != nil {
		log.Warnf("auth failed method=%s reason=api_key_lookup err=%v", method, err)
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
	}
	return &Principal{UserID: userID, FullAccess: false, Scopes: scopes}, nil
}

type streamWithContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *streamWithContext) Context() context.Context {
	return s.ctx
}

func extractBearerToken(ctx context.Context, method string, log AuthLogger) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		log.Warnf("auth failed method=%s reason=no_incoming_metadata", method)
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		log.Warnf("auth failed method=%s reason=no_authorization_in_metadata", method)
	}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		s, ok := strings.CutPrefix(v, "Bearer ")
		if !ok || s == "" {
			continue
		}
		s = strings.TrimSpace(s)
		if s != "" {
			return s, nil
		}
	}
	return "", status.Error(codes.Unauthenticated, "missing or invalid authorization header")
}
