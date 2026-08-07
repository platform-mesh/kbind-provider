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
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mchandler "sigs.k8s.io/multicluster-runtime/pkg/handler"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	kbpv1alpha1 "github.com/platform-mesh/kube-bind-provider/sdk/apis/kbind-provider/v1alpha1"
)

const (
	leaseNamespace       = "kbind"
	leaseDurationSeconds = 60

	leaseCleanupFinalizer = "kbind-provider.platform-mesh.io/lease-cleanup"

	condConnected = "Connected"
	condReady     = "Ready"

	reasonLeaseNotFound = "LeaseNotFound"
	reasonLeaseRenewed  = "LeaseRenewed"
	reasonLeaseStale    = "LeaseStale"
	reasonAsExpected    = "AsExpected"

	// leaseConnectionIndex is the cache field-index name for
	// Lease.metadata.annotations["core.kbind.io/connection"].
	leaseConnectionIndex = "lease.connection"
)

// ConnectedClusterReconciler watches ConnectedCluster objects and the heartbeat Leases
// the konnector maintains on the provider cluster. It updates ConnectedCluster.status
// to reflect whether the consumer is actively connected.
type ConnectedClusterReconciler struct {
	manager mcmanager.Manager
}

func NewConnectedClusterController() (*ConnectedClusterReconciler, error) {
	return &ConnectedClusterReconciler{}, nil
}

func (r *ConnectedClusterReconciler) SetupWithManager(mgr mcmanager.Manager) error {
	r.manager = mgr

	if err := mgr.GetFieldIndexer().IndexField(
		context.TODO(),
		&coordinationv1.Lease{},
		leaseConnectionIndex,
		func(obj client.Object) []string {
			lease, ok := obj.(*coordinationv1.Lease)
			if !ok {
				return nil
			}
			if v := lease.Annotations["core.kbind.io/connection"]; v != "" {
				return []string{v}
			}
			return nil
		},
	); err != nil {
		return fmt.Errorf("indexing lease connection: %w", err)
	}

	inKbindNS := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetNamespace() == leaseNamespace
	})

	return mcbuilder.ControllerManagedBy(mgr).
		Named("connectedcluster-controller").
		For(&kbpv1alpha1.ConnectedCluster{}).
		Watches(
			&coordinationv1.Lease{},
			mapLease,
			mcbuilder.WithPredicates(inKbindNS),
		).
		Complete(r)
}

func mapLease(clusterName multicluster.ClusterName, cl cluster.Cluster) handler.TypedEventHandler[client.Object, mcreconcile.Request] {
	return mchandler.TypedEnqueueRequestsFromMapFuncWithClusterPreservation(func(ctx context.Context, obj client.Object) []mcreconcile.Request {
		lease, ok := obj.(*coordinationv1.Lease)
		if !ok {
			return nil
		}
		if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
			return nil
		}

		if lease.Annotations == nil {
			return nil
		}
		if lease.Annotations["core.kbind.io/consumer-cluster-uid"] == "" {
			return nil
		}
		ccName := lease.Annotations["core.kbind.io/connection"]
		if ccName == "" {
			return nil
		}

		var cc kbpv1alpha1.ConnectedCluster
		if err := cl.GetClient().Get(ctx, types.NamespacedName{Name: ccName}, &cc); err != nil {
			log.FromContext(ctx).Error(err, "getting ConnectedCluster for Lease event")
			return nil
		}

		return []mcreconcile.Request{
			{
				ClusterName: clusterName,
				Request:     reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&cc)},
			},
		}
	})
}

// Reconcile drives a ConnectedCluster toward an accurate status: it finds the
// heartbeat Lease matching status.localClusterUID and sets the Connected and
// Ready conditions accordingly. It requeues every leaseDurationSeconds so a
// silently-dead konnector (no Lease delete event) is eventually detected.
func (r *ConnectedClusterReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	cl, err := r.manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting cluster %s: %w", req.ClusterName, err)
	}
	c := cl.GetClient()

	cc := &kbpv1alpha1.ConnectedCluster{}
	if err := c.Get(ctx, req.NamespacedName, cc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !cc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.handleDeletion(ctx, c, cc)
	}

	if !controllerutil.ContainsFinalizer(cc, leaseCleanupFinalizer) {
		controllerutil.AddFinalizer(cc, leaseCleanupFinalizer)
		if err := c.Update(ctx, cc); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Snapshot status fields that must not be re-read after mutation.
	origLeaseRef := cc.Status.LeaseRef
	origLocalUID := cc.Status.LocalClusterUID
	origConds := append([]metav1.Condition(nil), cc.Status.Conditions...)
	var origLastHeartbeat *metav1.Time
	if cc.Status.LastHeartbeatTime != nil {
		t := *cc.Status.LastHeartbeatTime
		origLastHeartbeat = &t
	}

	if err := r.reconcileStatus(ctx, c, cc); err != nil {
		return ctrl.Result{}, err
	}

	if !statusEqual(origLocalUID, origLeaseRef, origConds, origLastHeartbeat, cc.Status) {
		if err := c.Status().Update(ctx, cc); err != nil {
			log.Error(err, "updating ConnectedCluster status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: leaseDurationSeconds * time.Second}, nil
}

// handleDeletion deletes the heartbeat Lease referenced in status (if any) and
// removes the cleanup finalizer so the ConnectedCluster can be garbage-collected.
func (r *ConnectedClusterReconciler) handleDeletion(ctx context.Context, c client.Client, cc *kbpv1alpha1.ConnectedCluster) error {
	if ref := cc.Status.LeaseRef; ref != nil {
		lease := &coordinationv1.Lease{}
		err := c.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, lease)
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting lease %s/%s: %w", ref.Namespace, ref.Name, err)
		}
		if err == nil {
			if err := c.Delete(ctx, lease); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting lease %s/%s: %w", ref.Namespace, ref.Name, err)
			}
		}
	}

	controllerutil.RemoveFinalizer(cc, leaseCleanupFinalizer)
	return c.Update(ctx, cc)
}

// reconcileStatus looks up the heartbeat Lease and updates the ConnectedCluster
// status fields and conditions in place. It does not requeue or return
// transient errors for missing Leases.
//
// When LocalClusterUID is not yet set the konnector's Lease is located via the
// leaseConnectionIndex (keyed by core.kbind.io/connection == cc.Name), and the
// UID is read from Lease.spec.holderIdentity so the two paths share the same
// connected/stale logic below.
func (r *ConnectedClusterReconciler) reconcileStatus(ctx context.Context, c client.Client, cc *kbpv1alpha1.ConnectedCluster) error {
	var lease *coordinationv1.Lease

	if cc.Status.LocalClusterUID == "" {
		found, err := r.findLeaseByConnection(ctx, c, cc.Name)
		if err != nil {
			return err
		}
		if found == nil || found.Spec.HolderIdentity == nil || *found.Spec.HolderIdentity == "" {
			cc.Status.LeaseRef = nil
			setCondition(cc, condConnected, metav1.ConditionFalse, reasonLeaseNotFound, "konnector has not established a heartbeat yet")
			setCondition(cc, condReady, metav1.ConditionFalse, reasonLeaseNotFound, "konnector has not established a heartbeat yet")
			return nil
		}
		cc.Status.LocalClusterUID = *found.Spec.HolderIdentity
		lease = found
	} else {
		leaseName := "consumer-" + cc.Status.LocalClusterUID
		found := &coordinationv1.Lease{}
		err := c.Get(ctx, types.NamespacedName{Namespace: leaseNamespace, Name: leaseName}, found)
		if apierrors.IsNotFound(err) {
			cc.Status.LeaseRef = nil
			setCondition(cc, condConnected, metav1.ConditionFalse, reasonLeaseNotFound, "konnector has not established a heartbeat yet")
			setCondition(cc, condReady, metav1.ConditionFalse, reasonLeaseNotFound, "konnector has not established a heartbeat yet")
			return nil
		}
		if err != nil {
			return fmt.Errorf("getting lease %s/%s: %w", leaseNamespace, leaseName, err)
		}
		lease = found
	}

	cc.Status.LeaseRef = &kbpv1alpha1.LocalLeaseRef{Namespace: leaseNamespace, Name: lease.Name}
	if lease.Spec.RenewTime != nil {
		t := metav1.NewTime(lease.Spec.RenewTime.Time)
		cc.Status.LastHeartbeatTime = &t
	}
	if isLeaseConnected(lease) {
		setCondition(cc, condConnected, metav1.ConditionTrue, reasonLeaseRenewed, "konnector is heartbeating")
		setCondition(cc, condReady, metav1.ConditionTrue, reasonAsExpected, "konnector is connected")
	} else {
		setCondition(cc, condConnected, metav1.ConditionFalse, reasonLeaseStale, "konnector has not renewed its heartbeat")
		setCondition(cc, condReady, metav1.ConditionFalse, reasonLeaseStale, "konnector heartbeat is stale")
	}
	return nil
}

// findLeaseByConnection returns the first Lease in the kbind namespace whose
// core.kbind.io/connection annotation matches ccName, or nil if none exists.
func (r *ConnectedClusterReconciler) findLeaseByConnection(ctx context.Context, c client.Client, ccName string) (*coordinationv1.Lease, error) {
	var list coordinationv1.LeaseList
	if err := c.List(ctx, &list,
		client.InNamespace(leaseNamespace),
		client.MatchingFields{leaseConnectionIndex: ccName},
	); err != nil {
		return nil, fmt.Errorf("listing leases for %s: %w", ccName, err)
	}
	if len(list.Items) == 0 {
		return nil, nil
	}
	return &list.Items[0], nil
}

// isLeaseConnected returns true when the Lease was renewed within the staleness
// deadline (2× leaseDurationSeconds), meaning the konnector is likely still alive.
func isLeaseConnected(lease *coordinationv1.Lease) bool {
	if lease.Spec.RenewTime == nil {
		return false
	}
	deadline := lease.Spec.RenewTime.Time.Add(2 * leaseDurationSeconds * time.Second)
	return time.Now().Before(deadline)
}

func setCondition(cc *kbpv1alpha1.ConnectedCluster, condType string, status metav1.ConditionStatus, reason, msg string) {
	apimeta.SetStatusCondition(&cc.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: cc.Generation,
	})
}

// statusEqual compares the reconciler-managed status fields, ignoring condition
// timestamps so a no-op reconcile does not trigger a spurious status update.
func statusEqual(origLocalUID string, origLeaseRef *kbpv1alpha1.LocalLeaseRef, origConds []metav1.Condition, origLastHeartbeat *metav1.Time, cur kbpv1alpha1.ConnectedClusterStatus) bool {
	if origLocalUID != cur.LocalClusterUID {
		return false
	}
	if !leaseRefEqual(origLeaseRef, cur.LeaseRef) {
		return false
	}
	if !timeEqual(origLastHeartbeat, cur.LastHeartbeatTime) {
		return false
	}
	for _, ct := range []string{condConnected, condReady} {
		oa := apimeta.FindStatusCondition(origConds, ct)
		ca := apimeta.FindStatusCondition(cur.Conditions, ct)
		if !condEqual(oa, ca) {
			return false
		}
	}
	return true
}

func timeEqual(a, b *metav1.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(b)
}

func leaseRefEqual(a, b *kbpv1alpha1.LocalLeaseRef) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Namespace == b.Namespace && a.Name == b.Name
}

func condEqual(a, b *metav1.Condition) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Status == b.Status && a.Reason == b.Reason && a.Message == b.Message
}
