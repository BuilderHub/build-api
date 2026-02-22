package main

import (
	"context"
	"flag"
	"net"
	"net/http"
	"os/signal"
	"syscall"

	buildapiv1 "github.com/builderhub/build-api/api/gen/buildapi/v1"
	"github.com/builderhub/build-api/internal/server"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	grpcAddr := flag.String("grpc-addr", ":9090", "gRPC listen address")
	httpAddr := flag.String("http-addr", ":8080", "HTTP health listen address")
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig (empty for in-cluster)")
	flag.Parse()

	log, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer log.Sync()
	sugar := log.Sugar()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	k8sClient, err := server.NewK8sClient(*kubeconfig)
	if err != nil {
		log.Fatal("failed to create k8s client", zap.Error(err))
	}

	svc := server.NewBuildAPIService(k8sClient, sugar)
	srv := grpc.NewServer()
	buildapiv1.RegisterBuildAPIServer(srv, svc)
	reflection.Register(srv)

	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatal("failed to listen", zap.String("addr", *grpcAddr), zap.Error(err))
	}

	go func() {
		log.Info("gRPC listening", zap.String("addr", *grpcAddr))
		if err := srv.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			log.Error("gRPC serve error", zap.Error(err))
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	httpSrv := &http.Server{Addr: *httpAddr, Handler: mux}
	go func() {
		log.Info("HTTP health listening", zap.String("addr", *httpAddr))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP serve error", zap.Error(err))
		}
	}()

	<-ctx.Done()
	log.Info("shutting down...")
	srv.GracefulStop()
	_ = httpSrv.Shutdown(context.Background())
}
