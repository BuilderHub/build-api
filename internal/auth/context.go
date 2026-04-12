package auth

import "context"

type ctxKey int

const principalKey ctxKey = iota

// Principal holds authenticated identity: JWT sessions have FullAccess; API keys carry Scopes.
type Principal struct {
	UserID     string
	FullAccess bool
	Scopes     []string
}

// WithPrincipal attaches a principal to the context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// WithUserID sets a JWT-authenticated user (full access to org/build APIs per interceptor rules).
func WithUserID(ctx context.Context, userID string) context.Context {
	return WithPrincipal(ctx, &Principal{UserID: userID, FullAccess: true})
}

// PrincipalFromContext returns the principal or nil.
func PrincipalFromContext(ctx context.Context) *Principal {
	v, _ := ctx.Value(principalKey).(*Principal)
	return v
}

// UserIDFromContext returns the authenticated user id, if any.
func UserIDFromContext(ctx context.Context) string {
	p := PrincipalFromContext(ctx)
	if p == nil {
		return ""
	}
	return p.UserID
}
