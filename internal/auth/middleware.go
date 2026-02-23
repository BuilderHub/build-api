package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// PublicMethods are RPC full names that do not require authentication.
var PublicMethods = map[string]bool{
	"/buildapi.v1.AuthService/Register":    true,
	"/buildapi.v1.AuthService/Login":       true,
	"/buildapi.v1.AuthService/RefreshToken": true,
	"/buildapi.v1.BuildAPI/HealthCheck":    true,
}

// UnaryServerInterceptor returns a gRPC unary interceptor that validates JWT
// for protected methods and injects user ID into context.
func UnaryServerInterceptor(jwt *JWTManager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if PublicMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		token, err := extractBearerToken(ctx)
		if err != nil {
			return nil, err
		}
		claims, err := jwt.ValidateAccessToken(token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		ctx = WithUserID(ctx, claims.UserID)
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns a gRPC stream interceptor for JWT auth.
func StreamServerInterceptor(jwt *JWTManager) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if PublicMethods[info.FullMethod] {
			return handler(srv, ss)
		}
		ctx := ss.Context()
		token, err := extractBearerToken(ctx)
		if err != nil {
			return err
		}
		claims, err := jwt.ValidateAccessToken(token)
		if err != nil {
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

func extractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		vals = md.Get("grpcgateway-authorization")
	}
	for _, v := range vals {
		if s, ok := strings.CutPrefix(strings.TrimSpace(v), "Bearer "); ok && s != "" {
			return s, nil
		}
	}
	return "", status.Error(codes.Unauthenticated, "missing or invalid authorization header")
}
