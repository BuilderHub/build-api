package server

import (
	"context"
	"fmt"
	"time"

	buildapiv1 "github.com/builderhub/build-api/api/gen/buildapi/v1"
	buildkitv1alpha1 "github.com/builderhub/build-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

const lastUsedAnnotation = "builder-hub.dev/last-used"

// BuildAPIService implements buildapiv1.BuildAPIServer.
// Logger is a minimal interface for structured logging.
type Logger interface {
	Infow(msg string, keysAndValues ...interface{})
	Errorw(msg string, keysAndValues ...interface{})
}

type BuildAPIService struct {
	buildapiv1.UnimplementedBuildAPIServer
	k8s *K8sClient
	log Logger
}

// NewBuildAPIService creates a new BuildAPIService.
func NewBuildAPIService(k8s *K8sClient, log Logger) *BuildAPIService {
	return &BuildAPIService{k8s: k8s, log: log}
}

// ListBuilders returns all BuildkitBuilder CRs in a namespace.
func (s *BuildAPIService) ListBuilders(ctx context.Context, req *buildapiv1.ListBuildersRequest) (*buildapiv1.ListBuildersResponse, error) {
	var list buildkitv1alpha1.BuildkitBuilderList
	opts := []ctrl.ListOption{}
	if req.Namespace != "" {
		opts = append(opts, ctrl.InNamespace(req.Namespace))
	}
	if err := s.k8s.List(ctx, &list, opts...); err != nil {
		return nil, fmt.Errorf("list builders: %w", err)
	}
	builders := make([]*buildapiv1.Builder, 0, len(list.Items))
	for i := range list.Items {
		builders = append(builders, crToProto(&list.Items[i]))
	}
	return &buildapiv1.ListBuildersResponse{Builders: builders}, nil
}

// GetBuilder returns a single BuildkitBuilder by name.
func (s *BuildAPIService) GetBuilder(ctx context.Context, req *buildapiv1.GetBuilderRequest) (*buildapiv1.GetBuilderResponse, error) {
	var b buildkitv1alpha1.BuildkitBuilder
	if err := s.k8s.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &b); err != nil {
		return nil, fmt.Errorf("get builder: %w", err)
	}
	return &buildapiv1.GetBuilderResponse{Builder: crToProto(&b)}, nil
}

// CreateBuilder creates a new BuildkitBuilder CR.
func (s *BuildAPIService) CreateBuilder(ctx context.Context, req *buildapiv1.CreateBuilderRequest) (*buildapiv1.CreateBuilderResponse, error) {
	b := &buildkitv1alpha1.BuildkitBuilder{
		ObjectMeta: metav1.ObjectMeta{Namespace: req.Namespace, Name: req.Name},
		Spec:       protoSpecToCR(req.Spec),
	}
	if err := s.k8s.Create(ctx, b); err != nil {
		return nil, fmt.Errorf("create builder: %w", err)
	}
	s.log.Infow("created builder", "namespace", req.Namespace, "name", req.Name)
	return &buildapiv1.CreateBuilderResponse{Builder: crToProto(b)}, nil
}

// WakeBuilder patches builder-hub.dev/last-used for sleepy builders.
func (s *BuildAPIService) WakeBuilder(ctx context.Context, req *buildapiv1.WakeBuilderRequest) (*buildapiv1.WakeBuilderResponse, error) {
	var b buildkitv1alpha1.BuildkitBuilder
	if err := s.k8s.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &b); err != nil {
		return nil, fmt.Errorf("get builder: %w", err)
	}
	original := b.DeepCopy()
	modified := b.DeepCopy()
	if modified.Annotations == nil {
		modified.Annotations = make(map[string]string)
	}
	modified.Annotations[lastUsedAnnotation] = time.Now().UTC().Format(time.RFC3339)
	if err := s.k8s.Patch(ctx, modified, ctrl.MergeFrom(original)); err != nil {
		return nil, fmt.Errorf("patch builder: %w", err)
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
