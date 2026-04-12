package organizations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	buildapiv1 "github.com/builderhub/build-api/api/gen/buildapi/v1"
	"github.com/builderhub/build-api/internal/auth"
	"github.com/builderhub/build-api/internal/db"
	"github.com/builderhub/build-api/internal/k8s"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var slugRegex = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const validPlans = "starter, pro, enterprise"

type Service struct {
	buildapiv1.UnimplementedOrganizationServiceServer
	pool *db.Pool
	k8s  *k8s.Client
	log  *zap.SugaredLogger
}

func NewService(pool *db.Pool, k8sClient *k8s.Client, log *zap.SugaredLogger) *Service {
	return &Service{pool: pool, k8s: k8sClient, log: log}
}

func (s *Service) ListOrganizations(ctx context.Context, req *buildapiv1.ListOrganizationsRequest) (*buildapiv1.ListOrganizationsResponse, error) {
	userID := auth.UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}

	rows, err := s.pool.Query(ctx, `
		SELECT o.id, o.name, o.slug, o.plan, EXTRACT(EPOCH FROM o.created_at)::bigint,
			(SELECT COUNT(*)::bigint FROM organization_members om WHERE om.organization_id = o.id)
		FROM organizations o
		INNER JOIN organization_members m ON m.organization_id = o.id
		WHERE m.user_id = $1
		ORDER BY o.name
	`, userID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list organizations: %v", err)
	}
	defer rows.Close()

	var orgs []*buildapiv1.Organization
	for rows.Next() {
		var id, name, slug, plan string
		var createdAt, memberCount int64
		if err := rows.Scan(&id, &name, &slug, &plan, &createdAt, &memberCount); err != nil {
			return nil, status.Errorf(codes.Internal, "scan organization: %v", err)
		}
		bc := s.countBuildersInNamespace(ctx, id)
		orgs = append(orgs, &buildapiv1.Organization{
			Id:             id,
			Name:           name,
			Slug:           slug,
			Plan:           plan,
			CreatedAt:      createdAt,
			BuilderCount:   bc,
			TotalMinutes:   0,
			MonthlyMinutes: 0,
			MemberCount:    int32(memberCount),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "iter organizations: %v", err)
	}

	return &buildapiv1.ListOrganizationsResponse{Organizations: orgs}, nil
}

func (s *Service) GetOrganization(ctx context.Context, req *buildapiv1.GetOrganizationRequest) (*buildapiv1.GetOrganizationResponse, error) {
	userID := auth.UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	var id, name, slug, plan string
	var createdAt, memberCount int64
	err := s.pool.QueryRow(ctx, `
		SELECT o.id, o.name, o.slug, o.plan, EXTRACT(EPOCH FROM o.created_at)::bigint,
			(SELECT COUNT(*)::bigint FROM organization_members om WHERE om.organization_id = o.id)
		FROM organizations o
		INNER JOIN organization_members m ON m.organization_id = o.id
		WHERE o.id = $1 AND m.user_id = $2
	`, req.Id, userID).Scan(&id, &name, &slug, &plan, &createdAt, &memberCount)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, status.Error(codes.NotFound, "organization not found")
		}
		return nil, status.Errorf(codes.Internal, "get organization: %v", err)
	}

	bc := s.countBuildersInNamespace(ctx, id)

	return &buildapiv1.GetOrganizationResponse{
		Organization: &buildapiv1.Organization{
			Id:             id,
			Name:           name,
			Slug:           slug,
			Plan:           plan,
			CreatedAt:      createdAt,
			BuilderCount:   bc,
			TotalMinutes:   0,
			MonthlyMinutes: 0,
			MemberCount:    int32(memberCount),
		},
	}, nil
}

func (s *Service) CreateOrganization(ctx context.Context, req *buildapiv1.CreateOrganizationRequest) (*buildapiv1.CreateOrganizationResponse, error) {
	userID := auth.UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if strings.TrimSpace(req.Slug) == "" {
		return nil, status.Error(codes.InvalidArgument, "slug is required")
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !slugRegex.MatchString(slug) {
		return nil, status.Error(codes.InvalidArgument, "slug must be lowercase letters, numbers, and hyphens only")
	}
	plan := strings.TrimSpace(req.Plan)
	if plan == "" {
		plan = "starter"
	}
	if plan != "starter" && plan != "pro" && plan != "enterprise" {
		return nil, status.Errorf(codes.InvalidArgument, "plan must be one of: %s", validPlans)
	}

	name := strings.TrimSpace(req.Name)
	var orgID string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO organizations (name, slug, plan) VALUES ($1, $2, $3) RETURNING id
	`, name, slug, plan).Scan(&orgID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, status.Error(codes.AlreadyExists, "slug already in use")
		}
		return nil, status.Errorf(codes.Internal, "create organization: %v", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO organization_members (user_id, organization_id, role) VALUES ($1, $2, 'owner')
	`, userID, orgID)
	if err != nil {
		_, _ = s.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		return nil, status.Errorf(codes.Internal, "add owner membership: %v", err)
	}

	if err := s.k8s.EnsureOrgNamespace(ctx, orgID); err != nil {
		_, _ = s.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		s.log.Errorw("ensure org namespace", "err", err, "orgId", orgID)
		return nil, status.Errorf(codes.Internal, "create organization namespace: %v", err)
	}

	var createdAt int64
	_ = s.pool.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM created_at)::bigint FROM organizations WHERE id = $1`, orgID).Scan(&createdAt)

	bc := s.countBuildersInNamespace(ctx, orgID)

	s.log.Infow("organization created", "id", orgID, "slug", slug, "owner", userID)
	return &buildapiv1.CreateOrganizationResponse{
		Organization: &buildapiv1.Organization{
			Id:             orgID,
			Name:           name,
			Slug:           slug,
			Plan:           plan,
			CreatedAt:      createdAt,
			BuilderCount:   bc,
			TotalMinutes:   0,
			MonthlyMinutes: 0,
			MemberCount:    1,
		},
	}, nil
}

func (s *Service) UpdateOrganization(ctx context.Context, req *buildapiv1.UpdateOrganizationRequest) (*buildapiv1.UpdateOrganizationResponse, error) {
	userID := auth.UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	// Require admin or owner role
	var role string
	err := s.pool.QueryRow(ctx, `
		SELECT role FROM organization_members WHERE user_id = $1 AND organization_id = $2
	`, userID, req.Id).Scan(&role)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, status.Error(codes.NotFound, "organization not found")
		}
		return nil, status.Errorf(codes.Internal, "check membership: %v", err)
	}
	if role != "owner" && role != "admin" {
		return nil, status.Error(codes.PermissionDenied, "must be owner or admin to update organization")
	}

	if req.Name == "" && req.Slug == "" && req.Plan == "" {
		resp, err := s.GetOrganization(ctx, &buildapiv1.GetOrganizationRequest{Id: req.Id})
		if err != nil {
			return nil, err
		}
		return &buildapiv1.UpdateOrganizationResponse{Organization: resp.Organization}, nil
	}

	if req.Slug != "" {
		slug := strings.ToLower(strings.TrimSpace(req.Slug))
		if !slugRegex.MatchString(slug) {
			return nil, status.Error(codes.InvalidArgument, "slug must be lowercase letters, numbers, and hyphens only")
		}
	}
	if req.Plan != "" && req.Plan != "starter" && req.Plan != "pro" && req.Plan != "enterprise" {
		return nil, status.Errorf(codes.InvalidArgument, "plan must be one of: %s", validPlans)
	}

	var setClause []string
	var args []any
	argNum := 1
	if req.Name != "" {
		argNum++
		setClause = append(setClause, "name = $"+fmt.Sprintf("%d", argNum))
		args = append(args, strings.TrimSpace(req.Name))
	}
	if req.Slug != "" {
		argNum++
		setClause = append(setClause, "slug = $"+fmt.Sprintf("%d", argNum))
		args = append(args, strings.ToLower(strings.TrimSpace(req.Slug)))
	}
	if req.Plan != "" {
		argNum++
		setClause = append(setClause, "plan = $"+fmt.Sprintf("%d", argNum))
		args = append(args, req.Plan)
	}
	args = append([]any{req.Id}, args...)
	q := `UPDATE organizations SET ` + strings.Join(setClause, ", ") + ` WHERE id = $1`
	_, err = s.pool.Exec(ctx, q, args...)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, status.Error(codes.AlreadyExists, "slug already in use")
		}
		return nil, status.Errorf(codes.Internal, "update organization: %v", err)
	}

	resp, err := s.GetOrganization(ctx, &buildapiv1.GetOrganizationRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	return &buildapiv1.UpdateOrganizationResponse{Organization: resp.Organization}, nil
}

func (s *Service) DeleteOrganization(ctx context.Context, req *buildapiv1.DeleteOrganizationRequest) (*buildapiv1.DeleteOrganizationResponse, error) {
	userID := auth.UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	var role string
	err := s.pool.QueryRow(ctx, `
		SELECT role FROM organization_members WHERE user_id = $1 AND organization_id = $2
	`, userID, req.Id).Scan(&role)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, status.Error(codes.NotFound, "organization not found")
		}
		return nil, status.Errorf(codes.Internal, "check membership: %v", err)
	}
	if role != "owner" {
		return nil, status.Error(codes.PermissionDenied, "only the organization owner can delete the organization")
	}

	if err := s.k8s.DeleteOrgNamespace(ctx, req.Id); err != nil {
		s.log.Errorw("delete org namespace", "err", err, "orgId", req.Id)
		return nil, status.Errorf(codes.Internal, "delete organization namespace: %v", err)
	}

	tag, err := s.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "delete organization: %v", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, status.Error(codes.NotFound, "organization not found")
	}

	s.log.Infow("organization deleted", "id", req.Id, "deletedBy", userID)
	return &buildapiv1.DeleteOrganizationResponse{}, nil
}

// ListOrganizationMembers returns users in the organization; caller must be a member.
func (s *Service) ListOrganizationMembers(ctx context.Context, req *buildapiv1.ListOrganizationMembersRequest) (*buildapiv1.ListOrganizationMembersResponse, error) {
	userID := auth.UserIDFromContext(ctx)
	if userID == "" {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	orgID := strings.TrimSpace(req.GetOrganizationId())
	if orgID == "" {
		return nil, status.Error(codes.InvalidArgument, "organization_id is required")
	}

	var member bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM organization_members WHERE user_id = $1 AND organization_id = $2
		)
	`, userID, orgID).Scan(&member)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check membership: %v", err)
	}
	if !member {
		return nil, status.Error(codes.PermissionDenied, "not a member of this organization")
	}

	rows, err := s.pool.Query(ctx, `
		SELECT u.id::text, u.email, u.name, m.role, EXTRACT(EPOCH FROM m.joined_at)::bigint
		FROM organization_members m
		INNER JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = $1
		ORDER BY m.joined_at
	`, orgID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list members: %v", err)
	}
	defer rows.Close()

	var members []*buildapiv1.OrganizationMember
	for rows.Next() {
		var uid, email, name, role string
		var joinedAt int64
		if err := rows.Scan(&uid, &email, &name, &role, &joinedAt); err != nil {
			return nil, status.Errorf(codes.Internal, "scan member: %v", err)
		}
		members = append(members, &buildapiv1.OrganizationMember{
			UserId:   uid,
			Email:    email,
			Name:     name,
			Role:     role,
			JoinedAt: joinedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "iter members: %v", err)
	}

	return &buildapiv1.ListOrganizationMembersResponse{Members: members}, nil
}

func (s *Service) countBuildersInNamespace(ctx context.Context, orgID string) int32 {
	n, err := s.k8s.CountBuildkitBuildersInNamespace(ctx, orgID)
	if err != nil {
		s.log.Warnf("count builders in namespace: %v", err)
		return 0
	}
	return n
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return err != nil && errors.As(err, &pgErr) && pgErr.Code == "23505"
}
