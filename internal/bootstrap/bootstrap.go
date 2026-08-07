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

// Package bootstrap applies embedded YAML manifests to a kcp workspace via the
// dynamic client.  It is a self-contained reimplementation of the relevant
// subset of github.com/kcp-dev/kcp/config/helpers, avoiding the transitive
// dependency on k8s.io/apiextensions-apiserver (and, through it, the
// kcp-dev/client-go informers that reference API groups removed in the
// kcp-patched Kubernetes fork).
package bootstrap

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// Bootstrap reads all YAML manifests from efs, sorts them into application
// groups by resource kind priority, and applies each group sequentially,
// retrying until successful or ctx is cancelled.
func Bootstrap(ctx context.Context, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface, efs embed.FS) error {
	cache := memory.NewMemCacheClient(discoveryClient)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cache)

	resources, err := readResourcesFromFS(efs)
	if err != nil {
		return err
	}

	for _, group := range groupByHierarchy(resources) {
		if err := bootstrapGroup(ctx, dynamicClient, mapper, group, cache.Invalidate); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapGroup(ctx context.Context, dynamicClient dynamic.Interface, mapper meta.RESTMapper, resources []*unstructured.Unstructured, reset func()) error {
	return wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		if err := applyResources(ctx, dynamicClient, mapper, resources); err != nil {
			klog.FromContext(ctx).Info("failed to bootstrap resources, retrying", "err", err)
			reset()
			return false, nil
		}
		return true, nil
	})
}

func applyResources(ctx context.Context, dynamicClient dynamic.Interface, mapper meta.RESTMapper, resources []*unstructured.Unstructured) error {
	for _, obj := range resources {
		if err := applyResource(ctx, dynamicClient, mapper, obj); err != nil {
			return err
		}
	}
	return nil
}

const annotationCreateOnly = "bootstrap.kcp.io/create-only"

func applyResource(ctx context.Context, dynamicClient dynamic.Interface, mapper meta.RESTMapper, u *unstructured.Unstructured) error {
	log := klog.FromContext(ctx)
	gvk := u.GetObjectKind().GroupVersionKind()

	m, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("REST mapping for %s: %w", gvk, err)
	}

	u.SetResourceVersion("")

	upserted, err := dynamicClient.Resource(m.Resource).Namespace(u.GetNamespace()).Create(ctx, u, metav1.CreateOptions{})
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		existing, err := dynamicClient.Resource(m.Resource).Namespace(u.GetNamespace()).Get(ctx, u.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		if _, ok := existing.GetAnnotations()[annotationCreateOnly]; ok {
			log.Info("skipping update of create-only object", "kind", gvk.Kind, "name", u.GetName())
			return nil
		}
		u.SetResourceVersion(existing.GetResourceVersion())
		if _, err = dynamicClient.Resource(m.Resource).Namespace(u.GetNamespace()).Update(ctx, u, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update %s %s/%s: %w", gvk.Kind, u.GetNamespace(), u.GetName(), err)
		}
		log.Info("updated object", "kind", gvk.Kind, "name", u.GetName())
		return nil
	}

	log.Info("created object", "kind", gvk.Kind, "name", upserted.GetName())
	return nil
}

func readResourcesFromFS(efs embed.FS) ([]*unstructured.Unstructured, error) {
	files, err := efs.ReadDir(".")
	if err != nil {
		return nil, err
	}

	var result []*unstructured.Unstructured
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		data, err := efs.ReadFile(f.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f.Name(), err)
		}
		objs, err := parseYAML(data)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f.Name(), err)
		}
		result = append(result, objs...)
	}
	return result, nil
}

func parseYAML(data []byte) ([]*unstructured.Unstructured, error) {
	var results []*unstructured.Unstructured

	d := yamlutil.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	for {
		doc, err := d.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading yaml doc: %w", err)
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}

		jsonData, err := yaml.YAMLToJSON(doc)
		if err != nil {
			return nil, fmt.Errorf("yaml to json: %w", err)
		}

		u := &unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonData); err != nil {
			return nil, fmt.Errorf("unmarshaling object: %w", err)
		}

		results = append(results, u)
	}

	return results, nil
}

// defaultWeights defines the application order for kcp resource kinds.
// Earlier entries are applied first. Mirrors kcp/config/helpers.DefaultWeights.
var defaultWeights = []schema.GroupVersionKind{
	{Group: "apiextensions.k8s.io"},
	{Group: "core.kcp.io"},
	{Group: "tenancy.kcp.io", Kind: "WorkspaceType"},
	{Group: "tenancy.kcp.io"},
	{Group: "topology.kcp.io"},
	{Group: "apis.kcp.io", Kind: "APIResourceSchema"},
	{Group: "apis.kcp.io", Kind: "APIExport"},
	{Group: "apis.kcp.io", Kind: "APIBinding"},
	{Group: "apis.kcp.io"},
	{Kind: "Namespace"},
}

func groupByHierarchy(objects []*unstructured.Unstructured) [][]*unstructured.Unstructured {
	copied := slices.Clone(objects)
	slices.SortFunc(copied, func(a, b *unstructured.Unstructured) int {
		return cmp.Compare(objectWeight(a), objectWeight(b))
	})

	var groups [][]*unstructured.Unstructured
	curWeight := -1
	for _, obj := range copied {
		w := objectWeight(obj)
		if w != curWeight {
			curWeight = w
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], obj)
	}
	return groups
}

func objectWeight(obj *unstructured.Unstructured) int {
	gvk := obj.GroupVersionKind()
	for i, w := range defaultWeights {
		switch {
		case w.Group != "" && w.Kind != "":
			if w.Group == gvk.Group && w.Kind == gvk.Kind {
				return i
			}
		case w.Group != "":
			if w.Group == gvk.Group {
				return i
			}
		case w.Kind != "":
			if w.Kind == gvk.Kind {
				return i
			}
		}
	}
	return len(defaultWeights)
}
