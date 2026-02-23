package buildapi

import (
	"context"

	buildapiv1 "github.com/builderhub/build-api/api/gen/buildapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements BuildAPIServer with minimal handlers for auth-protected API.
type Server struct {
	buildapiv1.UnimplementedBuildAPIServer
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) ListBuilders(ctx context.Context, req *buildapiv1.ListBuildersRequest) (*buildapiv1.ListBuildersResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListBuilders not implemented")
}

func (s *Server) GetBuilder(ctx context.Context, req *buildapiv1.GetBuilderRequest) (*buildapiv1.GetBuilderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetBuilder not implemented")
}

func (s *Server) CreateBuilder(ctx context.Context, req *buildapiv1.CreateBuilderRequest) (*buildapiv1.CreateBuilderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "CreateBuilder not implemented")
}

func (s *Server) WakeBuilder(ctx context.Context, req *buildapiv1.WakeBuilderRequest) (*buildapiv1.WakeBuilderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "WakeBuilder not implemented")
}

func (s *Server) HealthCheck(ctx context.Context, req *buildapiv1.HealthCheckRequest) (*buildapiv1.HealthCheckResponse, error) {
	return &buildapiv1.HealthCheckResponse{Status: "ok"}, nil
}
