package auth

import (
	"context"
	"strings"

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
	"/buildapi.v1.AuthService/Login":       true,
	"/buildapi.v1.AuthService/RefreshToken": true,
	"/buildapi.v1.BuildAPI/HealthCheck":    true,
}

// UnaryServerInterceptor returns a gRPC unary interceptor that validates JWT
// for protected methods and injects user ID into context. log must be non-nil.
func UnaryServerInterceptor(jwt *JWTManager, log AuthLogger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if PublicMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		token, err := extractBearerToken(ctx, info.FullMethod, log)
		if err != nil {
			return nil, err
		}
		claims, err := jwt.ValidateAccessToken(token)
		if err != nil {
			log.Warnf("auth failed method=%s reason=token_validation err=%v", info.FullMethod, err)
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		ctx = WithUserID(ctx, claims.UserID)
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns a gRPC stream interceptor for JWT auth. log must be non-nil.
func StreamServerInterceptor(jwt *JWTManager, log AuthLogger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if PublicMethods[info.FullMethod] {
			return handler(srv, ss)
		}
		ctx := ss.Context()
		token, err := extractBearerToken(ctx, info.FullMethod, log)
		if err != nil {
			return err
		}
		claims, err := jwt.ValidateAccessToken(token)
		if err != nil {
			log.Warnf("auth failed method=%s reason=token_validation err=%v", info.FullMethod, err)
			return status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		ctx = WithUserID(ctx, claims.UserID)
		return handler(srv, &streamWithContext{ServerStream: ss, ctx: ctx})
	}
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
		s = strings.TrimSpace(s) // JWT must be 3 dot-separated segments; stray newlines/spaces cause "invalid number of segments"
		if s != "" {
			return s, nil
		}
	}
	return "", status.Error(codes.Unauthenticated, "missing or invalid authorization header")
}
