package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	buildapiv1 "github.com/builderhub/build-api/api/gen/buildapi/v1"
	"github.com/builderhub/build-api/internal/auth"
	"github.com/builderhub/build-api/internal/buildapi"
	"github.com/builderhub/build-api/internal/db"
	"github.com/builderhub/build-api/internal/organizations"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"
)

func main() {
	log := newLogger()
	defer log.Sync()
	sugar := log.Sugar()

	ctx := context.Background()

	databaseURL := getEnv("DATABASE_URL", "postgres://localhost/builderhub?sslmode=disable")
	jwtSecret := getEnv("JWT_SECRET", "dev-secret-change-in-production")
	grpcAddr := getEnv("GRPC_ADDR", ":9090")
	httpAddr := getEnv("HTTP_ADDR", ":8080")
	corsOrigins := parseCORSOrigins(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:3001,https://console.builder-hub.dev"))

	pool, err := db.NewPool(ctx, databaseURL)
	if err != nil {
		sugar.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	jwt := auth.NewJWTManager(jwtSecret)
	authSvc := auth.NewAuthService(pool, jwt, log.Sugar())
	buildAPISvc := buildapi.NewServer()
	orgSvc := organizations.NewService(pool, log.Sugar())

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(auth.UnaryServerInterceptor(jwt, sugar)),
		grpc.StreamInterceptor(auth.StreamServerInterceptor(jwt, sugar)),
	)
	buildapiv1.RegisterAuthServiceServer(grpcServer, authSvc)
	buildapiv1.RegisterBuildAPIServer(grpcServer, buildAPISvc)
	buildapiv1.RegisterOrganizationServiceServer(grpcServer, orgSvc)
	reflection.Register(grpcServer)

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		sugar.Fatalf("listen gRPC: %v", err)
	}
	defer lis.Close()

	go func() {
		sugar.Infof("gRPC server listening on %s", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			sugar.Errorf("gRPC server: %v", err)
		}
	}()

	gwMux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(forwardAuthHeader),
		runtime.WithMetadata(authMetadataAnnotator),
		runtime.WithForwardResponseOption(setAuthCookieForwardResponseOption()),
	)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := buildapiv1.RegisterAuthServiceHandlerFromEndpoint(ctx, gwMux, grpcAddr, opts); err != nil {
		sugar.Fatalf("register auth gateway: %v", err)
	}
	if err := buildapiv1.RegisterBuildAPIHandlerFromEndpoint(ctx, gwMux, grpcAddr, opts); err != nil {
		sugar.Fatalf("register buildapi gateway: %v", err)
	}
	if err := buildapiv1.RegisterOrganizationServiceHandlerFromEndpoint(ctx, gwMux, grpcAddr, opts); err != nil {
		sugar.Fatalf("register organization gateway: %v", err)
	}

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	rootMux.HandleFunc("/v1/auth/logout", logoutHandler(accessTokenCookieName))
	rootMux.Handle("/", gwMux)

	httpServer := &http.Server{Addr: httpAddr, Handler: corsHandler(rootMux, corsOrigins)}
	go func() {
		sugar.Infof("HTTP gateway listening on %s", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			sugar.Errorf("HTTP server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	grpcServer.GracefulStop()
	httpServer.Shutdown(context.Background())
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newLogger() *zap.Logger {
	cfg := zap.NewDevelopmentConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	cfg.EncoderConfig.TimeKey = ""
	log, _ := cfg.Build()
	return log
}

func logoutHandler(cookieName string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// setAuthCookieForwardResponseOption sets the access token cookie on login/register/refresh responses
// so the browser sends it automatically (proxies usually forward Cookie).
func setAuthCookieForwardResponseOption() func(context.Context, http.ResponseWriter, proto.Message) error {
	return func(_ context.Context, w http.ResponseWriter, resp proto.Message) error {
		var token string
		switch m := resp.(type) {
		case *buildapiv1.LoginResponse:
			token = m.GetAccessToken()
		case *buildapiv1.RegisterResponse:
			token = m.GetAccessToken()
		case *buildapiv1.RefreshTokenResponse:
			token = m.GetAccessToken()
		}
		if token == "" {
			return nil
		}
		http.SetCookie(w, &http.Cookie{
			Name:     accessTokenCookieName,
			Value:    token,
			Path:     "/",
			MaxAge:   3600, // 1 hour
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		return nil
	}
}

// forwardAuthHeader forwards Authorization to gRPC metadata.
func forwardAuthHeader(key string) (string, bool) {
	switch key {
	case "Authorization", "authorization":
		return "authorization", true
	default:
		return runtime.DefaultHeaderMatcher(key)
	}
}

const accessTokenCookieName = "builderhub_access_token"

// authMetadataAnnotator copies the auth token into gRPC metadata (Authorization header or cookie).
func authMetadataAnnotator(ctx context.Context, req *http.Request) metadata.MD {
	auth := req.Header.Get("Authorization")
	if auth == "" {
		if c, err := req.Cookie(accessTokenCookieName); err == nil && strings.TrimSpace(c.Value) != "" {
			t := strings.TrimSpace(c.Value)
			if !strings.HasPrefix(t, "Bearer ") {
				auth = "Bearer " + t
			} else {
				auth = t
			}
		}
	}
	if auth != "" {
		return metadata.Pairs("authorization", auth)
	}
	return nil
}

func parseCORSOrigins(s string) map[string]bool {
	allowed := make(map[string]bool)
	for _, o := range strings.Split(s, ",") {
		if o = strings.TrimSpace(o); o != "" {
			allowed[o] = true
		}
	}
	return allowed
}

func corsHandler(h http.Handler, allowedOrigins map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}
