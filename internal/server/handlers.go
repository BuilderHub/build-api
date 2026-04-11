package server

import (
	"context"
	"time"

	buildapiv1 "github.com/builderhub/build-api/api/gen/buildapi/v1"
	"github.com/builderhub/build-api/internal/auth"
	"github.com/builderhub/build-api/internal/db"
	buildkitv1alpha1 "github.com/builderhub/build-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const lastUsedAnnotation = "builder.builder-hub.dev/last-used"

// BuildAPIService implements buildapiv1.BuildAPIServer.
// Logger is a minimal interface for structured logging.
type Logger interface {
	Infow(msg string, keysAndValues ...interface{})
	Errorw(msg string, keysAndValues ...interface{})
	Warnf(format string, args ...interface{})
}

type BuildAPIService struct {
	buildapiv1.UnimplementedBuildAPIServer
	k8s  *K8sClient
	pool *db.Pool
	log  Logger
}

// NewBuildAPIService creates a new BuildAPIService.
func NewBuildAPIService(k8s *K8sClient, pool *db.Pool, log Logger) *BuildAPIService {
	return &BuildAPIService{k8s: k8s, pool: pool, log: log}
}

func (s *BuildAPIService) ensureOrgMember(ctx context.Context, namespace string) error {
	userID := auth.UserIDFromContext(ctx)
	if userID == "" {
		return status.Error(codes.Unauthenticated, "not authenticated")
	}
	if namespace == "" {
		return status.Error(codes.InvalidArgument, "namespace is required")
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM organization_members
			WHERE user_id = $1 AND organization_id = $2
		)
	`, userID, namespace).Scan(&exists)
	if err != nil {
		return status.Errorf(codes.Internal, "check org membership: %v", err)
	}
	if !exists {
		return status.Error(codes.PermissionDenied, "not a member of this organization")
	}
	return nil
}

// ListBuilders returns all BuildkitBuilder CRs in a namespace.
func (s *BuildAPIService) ListBuilders(ctx context.Context, req *buildapiv1.ListBuildersRequest) (*buildapiv1.ListBuildersResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	var list buildkitv1alpha1.BuildkitBuilderList
	opts := []ctrl.ListOption{}
	if req.Namespace != "" {
		opts = append(opts, ctrl.InNamespace(req.Namespace))
	}
	if err := s.k8s.List(ctx, &list, opts...); err != nil {
		s.log.Errorw("list builders", "err", err)
		return nil, status.Errorf(codes.Internal, "list builders: %v", err)
	}
	builders := make([]*buildapiv1.Builder, 0, len(list.Items))
	for i := range list.Items {
		builders = append(builders, crToProto(&list.Items[i]))
	}
	return &buildapiv1.ListBuildersResponse{Builders: builders}, nil
}

// GetBuilder returns a single BuildkitBuilder by name.
func (s *BuildAPIService) GetBuilder(ctx context.Context, req *buildapiv1.GetBuilderRequest) (*buildapiv1.GetBuilderResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	var b buildkitv1alpha1.BuildkitBuilder
	if err := s.k8s.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &b); err != nil {
		if ctrl.IgnoreNotFound(err) != nil {
			s.log.Errorw("get builder", "err", err)
			return nil, status.Errorf(codes.Internal, "get builder: %v", err)
		}
		return nil, status.Error(codes.NotFound, "builder not found")
	}
	return &buildapiv1.GetBuilderResponse{Builder: crToProto(&b)}, nil
}

// CreateBuilder creates a new BuildkitBuilder CR.
func (s *BuildAPIService) CreateBuilder(ctx context.Context, req *buildapiv1.CreateBuilderRequest) (*buildapiv1.CreateBuilderResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	if err := s.k8s.EnsureOrgNamespace(ctx, req.Namespace); err != nil {
		s.log.Errorw("ensure org namespace", "err", err, "namespace", req.Namespace)
		return nil, status.Errorf(codes.Internal, "ensure namespace: %v", err)
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.Spec == nil {
		return nil, status.Error(codes.InvalidArgument, "spec is required")
	}
	if req.Spec.TemplateRef == "" {
		return nil, status.Error(codes.InvalidArgument, "spec.template_ref is required (e.g. builder-small, builder-medium, builder-large, builder-xlarge)")
	}
	b := &buildkitv1alpha1.BuildkitBuilder{
		ObjectMeta: metav1.ObjectMeta{Namespace: req.Namespace, Name: req.Name},
		Spec:       protoSpecToCR(req.Spec),
	}
	if err := s.k8s.Create(ctx, b); err != nil {
		s.log.Errorw("create builder", "err", err)
		return nil, status.Errorf(codes.Internal, "create builder: %v", err)
	}
	s.log.Infow("created builder", "namespace", req.Namespace, "name", req.Name)
	return &buildapiv1.CreateBuilderResponse{Builder: crToProto(b)}, nil
}

// UpdateBuilder updates an existing BuildkitBuilder CR (spec only; name is immutable).
func (s *BuildAPIService) UpdateBuilder(ctx context.Context, req *buildapiv1.UpdateBuilderRequest) (*buildapiv1.UpdateBuilderResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.Spec == nil {
		return nil, status.Error(codes.InvalidArgument, "spec is required")
	}
	var b buildkitv1alpha1.BuildkitBuilder
	if err := s.k8s.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &b); err != nil {
		if ctrl.IgnoreNotFound(err) != nil {
			s.log.Errorw("get builder", "err", err)
			return nil, status.Errorf(codes.Internal, "get builder: %v", err)
		}
		return nil, status.Error(codes.NotFound, "builder not found")
	}
	b.Spec = protoSpecToCR(req.Spec)
	if err := s.k8s.Update(ctx, &b); err != nil {
		s.log.Errorw("update builder", "err", err)
		return nil, status.Errorf(codes.Internal, "update builder: %v", err)
	}
	s.log.Infow("updated builder", "namespace", req.Namespace, "name", req.Name)
	return &buildapiv1.UpdateBuilderResponse{Builder: crToProto(&b)}, nil
}

// DeleteBuilder deletes a BuildkitBuilder CR.
func (s *BuildAPIService) DeleteBuilder(ctx context.Context, req *buildapiv1.DeleteBuilderRequest) (*buildapiv1.DeleteBuilderResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	b := &buildkitv1alpha1.BuildkitBuilder{}
	b.Namespace = req.Namespace
	b.Name = req.Name
	if err := s.k8s.Delete(ctx, b); err != nil {
		if ctrl.IgnoreNotFound(err) != nil {
			s.log.Errorw("delete builder", "err", err)
			return nil, status.Errorf(codes.Internal, "delete builder: %v", err)
		}
	}
	s.log.Infow("deleted builder", "namespace", req.Namespace, "name", req.Name)
	return &buildapiv1.DeleteBuilderResponse{}, nil
}

// WakeBuilder patches builder.builder-hub.dev/last-used for sleepy builders.
func (s *BuildAPIService) WakeBuilder(ctx context.Context, req *buildapiv1.WakeBuilderRequest) (*buildapiv1.WakeBuilderResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	var b buildkitv1alpha1.BuildkitBuilder
	if err := s.k8s.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &b); err != nil {
		if ctrl.IgnoreNotFound(err) != nil {
			s.log.Errorw("get builder", "err", err)
			return nil, status.Errorf(codes.Internal, "get builder: %v", err)
		}
		return nil, status.Error(codes.NotFound, "builder not found")
	}
	original := b.DeepCopy()
	modified := b.DeepCopy()
	if modified.Annotations == nil {
		modified.Annotations = make(map[string]string)
	}
	modified.Annotations[lastUsedAnnotation] = time.Now().UTC().Format(time.RFC3339)
	if err := s.k8s.Patch(ctx, modified, ctrl.MergeFrom(original)); err != nil {
		s.log.Errorw("patch builder", "err", err)
		return nil, status.Errorf(codes.Internal, "wake builder: %v", err)
	}
	s.log.Infow("woke builder", "namespace", req.Namespace, "name", req.Name)
	return &buildapiv1.WakeBuilderResponse{Builder: crToProto(modified)}, nil
}

// HealthCheck returns health status.
func (s *BuildAPIService) HealthCheck(ctx context.Context, _ *buildapiv1.HealthCheckRequest) (*buildapiv1.HealthCheckResponse, error) {
	return &buildapiv1.HealthCheckResponse{Status: "ok"}, nil
}

func crToProto(b *buildkitv1alpha1.BuildkitBuilder) *buildapiv1.Builder {
	spec := &buildapiv1.BuilderSpec{
		Mode: string(b.Spec.Mode),
	}
	if b.Spec.TemplateRef != nil {
		spec.TemplateRef = *b.Spec.TemplateRef
	}
	if b.Spec.Replicas != nil {
		spec.Replicas = *b.Spec.Replicas
	} else {
		spec.Replicas = 1
	}
	if b.Spec.IdleTimeoutSeconds != nil {
		spec.IdleTimeoutSeconds = *b.Spec.IdleTimeoutSeconds
	} else {
		spec.IdleTimeoutSeconds = 300
	}
	if b.Spec.Labels != nil {
		spec.Labels = b.Spec.Labels
	}

	status := &buildapiv1.BuilderStatus{
		Endpoint: b.Status.Endpoint,
		NodePort: b.Status.NodePort,
		Phase:    b.Status.Phase,
	}
	return &buildapiv1.Builder{
		Namespace: b.Namespace,
		Name:      b.Name,
		Spec:      spec,
		Status:    status,
	}
}

func protoSpecToCR(s *buildapiv1.BuilderSpec) buildkitv1alpha1.BuildkitBuilderSpec {
	spec := buildkitv1alpha1.BuildkitBuilderSpec{
		Mode: buildkitv1alpha1.BuilderMode(s.Mode),
	}
	if s.TemplateRef != "" {
		spec.TemplateRef = ptr.To(s.TemplateRef)
	}
	if s.Replicas > 0 {
		spec.Replicas = ptr.To(s.Replicas)
	}
	if s.IdleTimeoutSeconds > 0 {
		spec.IdleTimeoutSeconds = ptr.To(s.IdleTimeoutSeconds)
	}
	if s.Labels != nil {
		spec.Labels = s.Labels
	}
	return spec
}
