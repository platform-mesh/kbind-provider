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

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	providerconfig "github.com/platform-mesh/kube-bind-provider/config/provider"
	"github.com/platform-mesh/kube-bind-provider/internal/bootstrap"
)

type initOptions struct {
	kcpKubeconfig string
}

func newInitCmd() *cobra.Command {
	o := &initOptions{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap provider and issuer resources in the kcp provider workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd.Context())
		},
	}
	cmd.Flags().StringVar(&o.kcpKubeconfig, "kcp-kubeconfig", "",
		"path to the kcp provider workspace kubeconfig")
	return cmd
}

func (o *initOptions) run(ctx context.Context) error {
	log := klog.FromContext(ctx)

	kcpConfig, err := clientcmd.BuildConfigFromFlags("", o.kcpKubeconfig)
	if err != nil {
		return fmt.Errorf("loading kcp kubeconfig: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kcpConfig)
	if err != nil {
		return fmt.Errorf("creating discovery client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(kcpConfig)
	if err != nil {
		return fmt.Errorf("creating dynamic client: %w", err)
	}

	log.Info("bootstrapping provider resources")
	if err := bootstrap.Bootstrap(ctx, discoveryClient, dynamicClient, providerconfig.FS); err != nil {
		return fmt.Errorf("bootstrapping provider resources: %w", err)
	}

	log.Info("bootstrap completed successfully")
	return nil
}
