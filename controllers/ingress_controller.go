/*


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

// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=oauth2.infra.doodle.com,resources=oauth2proxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oauth2.infra.doodle.com,resources=oauth2proxies/status,verbs=get;update;patch

package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	v1beta1 "github.com/DoodleScheduling/k8soauth2-controller/api/v1beta1"
	"github.com/DoodleScheduling/k8soauth2-controller/proxy"
)

const (
	serviceIndex = ".metadata.service"
)

// OAUTH2Proxy reconciles a OAUTH2Proxy object
type OAUTH2ProxyReconciler struct {
	client.Client
	HttpProxy *proxy.HttpProxy
	Log       logr.Logger
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
}

type OAUTH2ProxyReconcilerOptions struct {
	MaxConcurrentReconciles int
}

// SetupWithManager adding controllers
func (r *OAUTH2ProxyReconciler) SetupWithManager(mgr ctrl.Manager, opts OAUTH2ProxyReconcilerOptions) error {
	// Index the ReqeustClones by the Service references they point at
	if err := mgr.GetFieldIndexer().IndexField(context.TODO(), &v1beta1.OAUTH2Proxy{}, serviceIndex,
		func(o client.Object) []string {
			vb := o.(*v1beta1.OAUTH2Proxy)
			r.Log.Info(fmt.Sprintf("%s/%s", vb.GetNamespace(), vb.Spec.Backend.ServiceName))
			return []string{
				fmt.Sprintf("%s/%s", vb.GetNamespace(), vb.Spec.Backend.ServiceName),
			}
		},
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.OAUTH2Proxy{}).
		Watches(
			&source.Kind{Type: &v1.Service{}},
			handler.EnqueueRequestsFromMapFunc(r.requestsForServiceChange),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: opts.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *OAUTH2ProxyReconciler) requestsForServiceChange(o client.Object) []reconcile.Request {
	s, ok := o.(*v1.Service)
	if !ok {
		panic(fmt.Sprintf("expected a Service, got %T", o))
	}

	ctx := context.Background()
	var list v1beta1.OAUTH2ProxyList
	if err := r.List(ctx, &list, client.MatchingFields{
		serviceIndex: objectKey(s).String(),
	}); err != nil {
		return nil
	}

	var reqs []reconcile.Request
	for _, i := range list.Items {
		r.Log.Info("referenced service from a requestclone changed detected, reconcile requestclone", "namespace", i.GetNamespace(), "name", i.GetName())
		reqs = append(reqs, reconcile.Request{NamespacedName: objectKey(&i)})
	}

	return reqs
}

// Reconcile OAUTH2Proxys
func (r *OAUTH2ProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("Namespace", req.Namespace, "Name", req.NamespacedName)
	logger.Info("reconciling OAUTH2Proxy")

	// Fetch the OAUTH2Proxy instance
	ph := v1beta1.OAUTH2Proxy{}

	err := r.Client.Get(ctx, req.NamespacedName, &ph)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	ph, result, reconcileErr := r.reconcile(ctx, ph, logger)

	// Update status after reconciliation.
	if err = r.patchStatus(ctx, &ph); err != nil {
		logger.Error(err, "unable to update status after reconciliation")
		return ctrl.Result{Requeue: true}, err
	}

	return result, reconcileErr
}

func (r *OAUTH2ProxyReconciler) reconcile(ctx context.Context, ph v1beta1.OAUTH2Proxy, logger logr.Logger) (v1beta1.OAUTH2Proxy, ctrl.Result, error) {
	// Lookup matching service
	svc := v1.Service{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ph.GetNamespace(),
		Name:      ph.Spec.Backend.ServiceName,
	}, &svc)

	if err != nil {
		msg := "Service not found"
		r.Recorder.Event(&ph, "Normal", "info", msg)
		return v1beta1.OAUTH2ProxyNotReady(ph, v1beta1.ServiceNotFoundReason, msg), ctrl.Result{}, nil
	}

	var port int32
	for _, p := range svc.Spec.Ports {
		if p.Name == ph.Spec.Backend.ServicePort {
			port = p.Port
		}
	}

	if port == 0 {
		msg := "Port not found in service"
		r.Recorder.Event(&ph, "Normal", "info", msg)
		return v1beta1.OAUTH2ProxyNotReady(ph, v1beta1.ServicePortNotFoundReason, msg), ctrl.Result{}, nil
	}

	r.HttpProxy.RegisterOrUpdate(&proxy.OAUTH2Proxy{
		Host:        ph.Spec.Host,
		RedirectURI: ph.Spec.RedirectURI,
		Service:     svc.Spec.ClusterIP,
		Paths:       ph.Spec.Paths,
		Port:        port,
		Object: client.ObjectKey{
			Namespace: ph.GetNamespace(),
			Name:      ph.GetName(),
		},
	})

	msg := "Service backend successfully registered"
	r.Recorder.Event(&ph, "Normal", "info", msg)
	return v1beta1.OAUTH2ProxyReady(ph, v1beta1.ServiceBackendReadyReason, msg), ctrl.Result{}, err
}

func (r *OAUTH2ProxyReconciler) patchStatus(ctx context.Context, ph *v1beta1.OAUTH2Proxy) error {
	key := client.ObjectKeyFromObject(ph)
	latest := &v1beta1.OAUTH2Proxy{}
	if err := r.Client.Get(ctx, key, latest); err != nil {
		return err
	}

	return r.Client.Status().Patch(ctx, ph, client.MergeFrom(latest))
}

// objectKey returns client.ObjectKey for the object.
func objectKey(object metav1.Object) client.ObjectKey {
	return client.ObjectKey{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}
}
