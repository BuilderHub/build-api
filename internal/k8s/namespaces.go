package k8s

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnsureOrgNamespace creates the Kubernetes namespace for an organization (id = namespace name).
// Succeeds if the namespace already exists.
func (c *Client) EnsureOrgNamespace(ctx context.Context, orgID string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: orgID,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":     "builderhub",
				"builderhub.dev/organization-id": orgID,
			},
		},
	}
	err := c.Create(ctx, ns)
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// DeleteOrgNamespace deletes the organization namespace and all namespaced resources within it.
// Succeeds if the namespace does not exist.
func (c *Client) DeleteOrgNamespace(ctx context.Context, orgID string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: orgID},
	}
	err := c.Delete(ctx, ns)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
