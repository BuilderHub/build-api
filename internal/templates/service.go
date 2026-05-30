package templates

import (
	"context"

	buildapiv1 "github.com/builderhub/build-api/api/gen/buildapi/v1"
	"github.com/builderhub/build-api/internal/auth"
	"github.com/builderhub/build-api/internal/db"
	"github.com/builderhub/build-api/internal/k8s"
	templatev1alpha1 "github.com/builderhub/build-operator/api/buildertemplate/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	buildapiv1.UnimplementedTemplateServiceServer
	k8s  *k8s.Client
	pool *db.Pool
	log  *zap.SugaredLogger
}

func NewService(k8sClient *k8s.Client, pool *db.Pool, log *zap.SugaredLogger) *Service {
	return &Service{k8s: k8sClient, pool: pool, log: log}
}

// ListTemplates lists all BuildkitBuilderTemplates in the given organization namespace.
func (s *Service) ListTemplates(ctx context.Context, req *buildapiv1.ListTemplatesRequest) (*buildapiv1.ListTemplatesResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}

	var list templatev1alpha1.BuildkitBuilderTemplateList
	if err := s.k8s.List(ctx, &list, ctrl.InNamespace(req.Namespace)); err != nil {
		s.log.Errorw("list templates", "err", err, "namespace", req.Namespace)
		return nil, status.Errorf(codes.Internal, "list templates: %v", err)
	}

	templates := make([]*buildapiv1.BuilderTemplate, 0, len(list.Items))
	for i := range list.Items {
		templates = append(templates, templateCRToProto(&list.Items[i]))
	}
	return &buildapiv1.ListTemplatesResponse{Templates: templates}, nil
}

// GetTemplate returns a single template by name within the organization namespace.
func (s *Service) GetTemplate(ctx context.Context, req *buildapiv1.GetTemplateRequest) (*buildapiv1.GetTemplateResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	var tmpl templatev1alpha1.BuildkitBuilderTemplate
	if err := s.k8s.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &tmpl); err != nil {
		if ctrl.IgnoreNotFound(err) != nil {
			s.log.Errorw("get template", "err", err, "namespace", req.Namespace, "name", req.Name)
			return nil, status.Errorf(codes.Internal, "get template: %v", err)
		}
		return nil, status.Error(codes.NotFound, "template not found")
	}

	return &buildapiv1.GetTemplateResponse{Template: templateCRToProto(&tmpl)}, nil
}

// CreateTemplate creates a new BuildkitBuilderTemplate inside the organization namespace.
func (s *Service) CreateTemplate(ctx context.Context, req *buildapiv1.CreateTemplateRequest) (*buildapiv1.CreateTemplateResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.Spec == nil {
		return nil, status.Error(codes.InvalidArgument, "spec is required")
	}

	tmpl := &templatev1alpha1.BuildkitBuilderTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
		},
		Spec: protoSpecToTemplateSpec(req.Spec),
	}

	if err := s.k8s.Create(ctx, tmpl); err != nil {
		s.log.Errorw("create template", "err", err, "namespace", req.Namespace, "name", req.Name)
		return nil, status.Errorf(codes.Internal, "create template: %v", err)
	}

	s.log.Infow("created template", "namespace", req.Namespace, "name", req.Name)
	return &buildapiv1.CreateTemplateResponse{Template: templateCRToProto(tmpl)}, nil
}

// UpdateTemplate replaces the spec of an existing template in the organization namespace.
func (s *Service) UpdateTemplate(ctx context.Context, req *buildapiv1.UpdateTemplateRequest) (*buildapiv1.UpdateTemplateResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.Spec == nil {
		return nil, status.Error(codes.InvalidArgument, "spec is required")
	}

	var tmpl templatev1alpha1.BuildkitBuilderTemplate
	if err := s.k8s.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &tmpl); err != nil {
		if ctrl.IgnoreNotFound(err) != nil {
			s.log.Errorw("get template for update", "err", err, "namespace", req.Namespace, "name", req.Name)
			return nil, status.Errorf(codes.Internal, "get template: %v", err)
		}
		return nil, status.Error(codes.NotFound, "template not found")
	}

	tmpl.Spec = protoSpecToTemplateSpec(req.Spec)

	if err := s.k8s.Update(ctx, &tmpl); err != nil {
		s.log.Errorw("update template", "err", err, "namespace", req.Namespace, "name", req.Name)
		return nil, status.Errorf(codes.Internal, "update template: %v", err)
	}

	s.log.Infow("updated template", "namespace", req.Namespace, "name", req.Name)
	return &buildapiv1.UpdateTemplateResponse{Template: templateCRToProto(&tmpl)}, nil
}

// DeleteTemplate deletes a template within the organization namespace.
func (s *Service) DeleteTemplate(ctx context.Context, req *buildapiv1.DeleteTemplateRequest) (*buildapiv1.DeleteTemplateResponse, error) {
	if err := s.ensureOrgMember(ctx, req.Namespace); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	tmpl := &templatev1alpha1.BuildkitBuilderTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
		},
	}
	if err := s.k8s.Delete(ctx, tmpl); err != nil {
		if ctrl.IgnoreNotFound(err) != nil {
			s.log.Errorw("delete template", "err", err, "namespace", req.Namespace, "name", req.Name)
			return nil, status.Errorf(codes.Internal, "delete template: %v", err)
		}
	}

	s.log.Infow("deleted template", "namespace", req.Namespace, "name", req.Name)
	return &buildapiv1.DeleteTemplateResponse{}, nil
}

func (s *Service) ensureOrgMember(ctx context.Context, namespace string) error {
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

// --- mapping helpers (namespace aware) ---

func templateCRToProto(t *templatev1alpha1.BuildkitBuilderTemplate) *buildapiv1.BuilderTemplate {
	if t == nil {
		return &buildapiv1.BuilderTemplate{}
	}
	return &buildapiv1.BuilderTemplate{
		Namespace: t.Namespace,
		Name:      t.Name,
		Spec:      templateSpecToProto(&t.Spec),
	}
}

func templateSpecToProto(s *templatev1alpha1.BuildkitBuilderTemplateSpec) *buildapiv1.BuilderTemplateSpec {
	if s == nil {
		return &buildapiv1.BuilderTemplateSpec{}
	}
	spec := &buildapiv1.BuilderTemplateSpec{
		BuildkitImage: s.BuildkitImage,
		Rootless:      s.Rootless,
		Arch:          s.Arch,
		Resources:     resourceRequirementsToProto(s.Resources),
	}

	cc := &buildapiv1.CacheConfig{Type: string(s.CacheConfig.Type)}
	if s.CacheConfig.PVC != nil {
		cc.Pvc = &buildapiv1.PVCConfig{
			Size:             s.CacheConfig.PVC.Size,
			StorageClassName: s.CacheConfig.PVC.StorageClassName,
			AccessModes:      append([]string(nil), s.CacheConfig.PVC.AccessModes...),
		}
	}
	if s.CacheConfig.S3 != nil {
		cc.S3 = &buildapiv1.S3Config{
			Bucket:   s.CacheConfig.S3.Bucket,
			Region:   s.CacheConfig.S3.Region,
			Endpoint: s.CacheConfig.S3.Endpoint,
		}
	}
	spec.CacheConfig = cc

	return spec
}

func protoSpecToTemplateSpec(s *buildapiv1.BuilderTemplateSpec) templatev1alpha1.BuildkitBuilderTemplateSpec {
	if s == nil {
		return templatev1alpha1.BuildkitBuilderTemplateSpec{}
	}

	out := templatev1alpha1.BuildkitBuilderTemplateSpec{
		BuildkitImage: s.BuildkitImage,
		Rootless:      s.Rootless,
		Arch:          s.Arch,
		Resources:     protoToResourceRequirements(s.Resources),
	}

	if s.CacheConfig != nil {
		out.CacheConfig.Type = templatev1alpha1.CacheType(s.CacheConfig.Type)
		if s.CacheConfig.Pvc != nil {
			out.CacheConfig.PVC = &templatev1alpha1.PVCConfig{
				Size:             s.CacheConfig.Pvc.Size,
				StorageClassName: s.CacheConfig.Pvc.StorageClassName,
				AccessModes:      append([]string(nil), s.CacheConfig.Pvc.AccessModes...),
			}
		}
		if s.CacheConfig.S3 != nil {
			out.CacheConfig.S3 = &templatev1alpha1.S3Config{
				Bucket:   s.CacheConfig.S3.Bucket,
				Region:   s.CacheConfig.S3.Region,
				Endpoint: s.CacheConfig.S3.Endpoint,
			}
		}
	}

	return out
}

// --- resource conversion helpers (for template resources) ---

func resourceRequirementsToProto(rr corev1.ResourceRequirements) *buildapiv1.ResourceRequirements {
	if len(rr.Limits) == 0 && len(rr.Requests) == 0 {
		return nil
	}
	pr := &buildapiv1.ResourceRequirements{}
	if len(rr.Limits) > 0 {
		pr.Limits = make(map[string]string, len(rr.Limits))
		for k, q := range rr.Limits {
			pr.Limits[string(k)] = q.String()
		}
	}
	if len(rr.Requests) > 0 {
		pr.Requests = make(map[string]string, len(rr.Requests))
		for k, q := range rr.Requests {
			pr.Requests[string(k)] = q.String()
		}
	}
	return pr
}

func protoToResourceRequirements(pr *buildapiv1.ResourceRequirements) corev1.ResourceRequirements {
	if pr == nil {
		return corev1.ResourceRequirements{}
	}
	rr := corev1.ResourceRequirements{}
	if len(pr.Limits) > 0 {
		rr.Limits = make(corev1.ResourceList, len(pr.Limits))
		for k, v := range pr.Limits {
			if q, err := resource.ParseQuantity(v); err == nil {
				rr.Limits[corev1.ResourceName(k)] = q
			}
		}
	}
	if len(pr.Requests) > 0 {
		rr.Requests = make(corev1.ResourceList, len(pr.Requests))
		for k, v := range pr.Requests {
			if q, err := resource.ParseQuantity(v); err == nil {
				rr.Requests[corev1.ResourceName(k)] = q
			}
		}
	}
	return rr
}
