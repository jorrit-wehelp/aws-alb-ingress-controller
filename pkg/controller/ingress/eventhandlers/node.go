package eventhandlers

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/aws-alb-ingress-controller/pkg/backend"
	"sigs.k8s.io/aws-alb-ingress-controller/pkg/ingress"
	"sigs.k8s.io/aws-alb-ingress-controller/pkg/k8s"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var nodeLogger = log.Log.WithName("eventhandlers").WithName("node")

func NewEnqueueRequestsForNodeEvent(ingGroupBuilder ingress.GroupBuilder, ingressClass string, k8sCache cache.Cache) handler.EventHandler {
	return &enqueueRequestsForNodeEvent{
		ingGroupBuilder: ingGroupBuilder,
		ingressClass:    ingressClass,
		k8sCache:        k8sCache,
	}
}

type enqueueRequestsForNodeEvent struct {
	ingGroupBuilder ingress.GroupBuilder
	ingressClass    string
	k8sCache        cache.Cache
}

// Create is called in response to an create event - e.g. Pod Creation.
func (h *enqueueRequestsForNodeEvent) Create(e event.CreateEvent, queue workqueue.RateLimitingInterface) {
	node := e.Object.(*corev1.Node)
	if backend.IsNodeSuitableAsTrafficProxy(node) {
		h.enqueueImpactedIngresses(queue)
	}
}

// Update is called in response to an update event -  e.g. Pod Updated.
func (h *enqueueRequestsForNodeEvent) Update(e event.UpdateEvent, queue workqueue.RateLimitingInterface) {
	nodeOld := e.ObjectOld.(*corev1.Node)
	nodeNew := e.ObjectNew.(*corev1.Node)
	if backend.IsNodeSuitableAsTrafficProxy(nodeOld) != backend.IsNodeSuitableAsTrafficProxy(nodeNew) {
		h.enqueueImpactedIngresses(queue)
	}
}

// Delete is called in response to a delete event - e.g. Pod Deleted.
func (h *enqueueRequestsForNodeEvent) Delete(e event.DeleteEvent, queue workqueue.RateLimitingInterface) {
	node := e.Object.(*corev1.Node)
	if backend.IsNodeSuitableAsTrafficProxy(node) {
		h.enqueueImpactedIngresses(queue)
	}
}

// Generic is called in response to an event of an unknown type or a synthetic event triggered as a cron or
// external trigger request - e.g. reconcile Autoscaling, or a Webhook.
func (h *enqueueRequestsForNodeEvent) Generic(e event.GenericEvent, queue workqueue.RateLimitingInterface) {
	h.enqueueImpactedIngresses(queue)
}

func (h *enqueueRequestsForNodeEvent) enqueueImpactedIngresses(queue workqueue.RateLimitingInterface) {
	ctx := context.Background()
	ingList := &extensions.IngressList{}
	if err := h.k8sCache.List(ctx, nil, ingList); err != nil {
		nodeLogger.Error(err, "failed to list Ingresses")
		return
	}

	groupIDs := sets.NewString()
	for i := range ingList.Items {
		ing := &ingList.Items[i]
		if !ingress.MatchesIngressClass(h.ingressClass, ing) {
			continue
		}
		groupID, err := h.ingGroupBuilder.BuildGroupID(ctx, ing)
		if err != nil {
			nodeLogger.Error(err, "failed to build ingress group ID", "ingress", k8s.NamespacedName(ing).String())
		}
		if !groupIDs.Has(groupID.String()) {
			groupIDs.Insert(groupID.String())
			queue.Add(groupID.EncodeToReconcileRequest())
		}
	}
}
