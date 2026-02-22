package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/builderhub/build-api/api/gen/buildapi/v1"
	"github.com/builderhub/build-api/internal/auth"
	"github.com/builderhub/build-api/internal/buildapi"
	"github.com/builderhub/build-api/internal/db"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
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

	pool, err := db.NewPool(ctx, databaseURL)
	if err != nil {
		sugar.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	jwt := auth.NewJWTManager(jwtSecret)
	authSvc := auth.NewAuthService(pool, jwt, log.Sugar())
	buildAPISvc := buildapi.NewServer()

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(auth.UnaryServerInterceptor(jwt)),
		grpc.StreamInterceptor(auth.StreamServerInterceptor(jwt)),
	)
	buildapiv1.RegisterAuthServiceServer(grpcServer, authSvc)
	buildapiv1.RegisterBuildAPIServer(grpcServer, buildAPISvc)
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
	)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := buildapiv1.RegisterAuthServiceHandlerFromEndpoint(ctx, gwMux, grpcAddr, opts); err != nil {
		sugar.Fatalf("register auth gateway: %v", err)
	}
	if err := buildapiv1.RegisterBuildAPIHandlerFromEndpoint(ctx, gwMux, grpcAddr, opts); err != nil {
		sugar.Fatalf("register buildapi gateway: %v", err)
	}

	httpServer := &http.Server{Addr: httpAddr, Handler: corsHandler(gwMux)}
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

// forwardAuthHeader forwards Authorization to gRPC metadata.
func forwardAuthHeader(key string) (string, bool) {
	switch key {
	case "Authorization", "authorization":
		return "authorization", true
	default:
		return runtime.DefaultHeaderMatcher(key)
	}
}

func corsHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}
