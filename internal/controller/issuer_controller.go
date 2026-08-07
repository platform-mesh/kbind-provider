/*
Copyright 2026 The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"net/url"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

const (
	kbindNamespace    = "kbind"
	konnectorSA       = "kbind-konnector"
	konnectorToken    = "kbind-konnector-token"
	credentialsSecret = "kbind-kubeconfig"
)

// IssuerReconciler provisions konnector credentials in each consumer workspace
// that has bound to the kbind APIExport.
type IssuerReconciler struct {
	manager mcmanager.Manager
}

func NewIssuerController() (*IssuerReconciler, error) {
	return &IssuerReconciler{}, nil
}

func (r *IssuerReconciler) SetupWithManager(mgr mcmanager.Manager) error {
	r.manager = mgr

	return mcbuilder.ControllerManagedBy(mgr).
		Named("issuer-controller").
		For(&apisv1alpha2.APIBinding{}).
		Complete(r)
}

func (r *IssuerReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("cluster", req.ClusterName)

	cl, err := r.manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("getting cluster: %w", err)
	}
	c := cl.GetClient()

	log.Info("provisioning konnector credentials")

	if err := r.ensureNamespace(ctx, c); err != nil {
		return reconcile.Result{}, err
	}
	if err := r.ensureServiceAccount(ctx, c); err != nil {
		return reconcile.Result{}, err
	}
	if err := r.ensureClusterRoleBinding(ctx, c); err != nil {
		return reconcile.Result{}, err
	}
	if err := r.ensureTokenSecret(ctx, c); err != nil {
		return reconcile.Result{}, err
	}

	token, err := r.readToken(ctx, c)
	if err != nil {
		return reconcile.Result{}, err
	}
	if len(token) == 0 {
		log.Info("token not yet available, requeuing")
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}

	consumerWsUrl, err := consumerWorkspaceURL(cl.GetConfig().Host, string(req.ClusterName))
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("building consumer workspace URL: %w", err)
	}

	kubeconfigBytes, err := buildKubeconfig(consumerWsUrl, cl.GetConfig().CAData, string(token))
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("building kubeconfig: %w", err)
	}

	creds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      credentialsSecret,
			Namespace: kbindNamespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, creds, func() error {
		creds.Data = map[string][]byte{"kubeconfig": kubeconfigBytes}
		return nil
	}); err != nil {
		return reconcile.Result{}, fmt.Errorf("upserting credentials secret: %w", err)
	}

	log.Info("konnector credentials ready")
	return reconcile.Result{}, nil
}

func consumerWorkspaceURL(vwHost, clusterName string) (string, error) {
	u, err := url.Parse(vwHost)
	if err != nil {
		return "", fmt.Errorf("parsing host %q: %w", vwHost, err)
	}
	return u.Scheme + "://" + u.Host + "/clusters/" + clusterName, nil
}

func (r *IssuerReconciler) ensureNamespace(ctx context.Context, c client.Client) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: kbindNamespace}}
	if err := c.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating namespace: %w", err)
	}
	return nil
}

func (r *IssuerReconciler) ensureServiceAccount(ctx context.Context, c client.Client) error {
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
		Name:      konnectorSA,
		Namespace: kbindNamespace,
	}}
	if err := c.Create(ctx, sa); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating service account: %w", err)
	}
	return nil
}

func (r *IssuerReconciler) ensureClusterRoleBinding(ctx context.Context, c client.Client) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: konnectorSA},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      konnectorSA,
			Namespace: kbindNamespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
	}
	if err := c.Create(ctx, crb); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating cluster role binding: %w", err)
	}
	return nil
}

func (r *IssuerReconciler) ensureTokenSecret(ctx context.Context, c client.Client) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      konnectorToken,
			Namespace: kbindNamespace,
			Annotations: map[string]string{
				corev1.ServiceAccountNameKey: konnectorSA,
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	if err := c.Create(ctx, secret); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating token secret: %w", err)
	}
	return nil
}

func (r *IssuerReconciler) readToken(ctx context.Context, c client.Client) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: kbindNamespace, Name: konnectorToken}, secret); err != nil {
		return nil, fmt.Errorf("reading token secret: %w", err)
	}
	return secret.Data["token"], nil
}

func buildKubeconfig(server string, caData []byte, token string) ([]byte, error) {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["consumer"] = &clientcmdapi.Cluster{
		Server:                   server,
		CertificateAuthorityData: caData,
	}
	cfg.AuthInfos[konnectorSA] = &clientcmdapi.AuthInfo{
		Token: token,
	}
	cfg.Contexts["consumer"] = &clientcmdapi.Context{
		Cluster:  "consumer",
		AuthInfo: konnectorSA,
	}
	cfg.CurrentContext = "consumer"
	return clientcmd.Write(*cfg)
}
