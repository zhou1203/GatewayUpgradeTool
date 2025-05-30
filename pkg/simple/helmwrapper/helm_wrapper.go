/*
Copyright 2020 The KubeSphere Authors.

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

package helmwrapper

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"

	"helm.sh/helm/v3/pkg/chartutil"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"k8s.io/klog/v2"
	kpath "k8s.io/utils/path"

	"errors"
)

const (
	workspaceBase = "/tmp/helm-operator"
)

var (
	UninstallNotFoundFormat = "uninstall: Release not loaded: %s: release: not found"
	StatusNotFoundFormat    = "release: not found"
	releaseExists           = "release exists"
)

type HelmRes struct {
	Message string
}

var _ HelmWrapper = &helmWrapper{}

type HelmWrapper interface {
	// Install a release
	Install(chartData, values []byte, chartName string) error
	// Upgrade a release
	Upgrade(chartData, values []byte, chartName string) error
	// InstallOrUpgrade a release
	InstallOrUpgrade(chartData, values []byte, chartName string) error
	// Uninstall a release
	Uninstall() error
	// Get manifests
	Manifest() (string, error)
	// IsReleaseReady check helm release is ready or not
	IsReleaseReady(timeout time.Duration) (bool, error)
}

func NewHelmWrapper(kubeconfig, ns, rls string, options ...Option) *helmWrapper {
	c := &helmWrapper{
		Kubeconfig:      kubeconfig,
		Namespace:       ns,
		ReleaseName:     rls,
		base:            workspaceBase,
		workspaceSuffix: "kubesphere",
	}

	klog.V(8).Infof("namespace: %s, name: %s, release: %s, kubeconfig:%s", c.Namespace, c.ReleaseName, rls, kubeconfig)
	getter := NewClusterRESTClientGetter(kubeconfig, ns)
	c.helmConf = new(action.Configuration)
	c.helmConf.Init(getter, ns, "", klog.Infof)

	for _, option := range options {
		option(c)
	}

	return c
}

// IsReleaseReady check helm releases is ready or not
// If the return values is (true, nil), then the resources are ready
func (c *helmWrapper) IsReleaseReady(waitTime time.Duration) (bool, error) {
	// Get the manifest to build resources
	manifest, err := c.Manifest()
	if err != nil {
		return false, err
	}

	client := c.helmConf.KubeClient
	resources, _ := client.Build(bytes.NewBufferString(manifest), true)

	if err := client.Wait(resources, waitTime); err != nil {
		return false, err
	}
	return true, nil
}

func (c *helmWrapper) Status() (*helmrelease.Release, error) {
	helmStatus := action.NewStatus(c.helmConf)
	rel, err := helmStatus.Run(c.ReleaseName)
	if err != nil {
		if err.Error() == StatusNotFoundFormat {
			klog.V(2).Infof("namespace: %s, name: %s, run command failed, error: %v", c.Namespace, c.ReleaseName, err)
			return nil, err
		}
		klog.Errorf("namespace: %s, name: %s, run command failed, error: %v", c.Namespace, c.ReleaseName, err)
		return nil, err
	}

	klog.V(2).Infof("namespace: %s, name: %s, run command success", c.Namespace, c.ReleaseName)
	klog.V(8).Infof("namespace: %s, name: %s, run command success, manifest: %s", c.Namespace, c.ReleaseName, rel.Manifest)
	return rel, nil
}

func (c *helmWrapper) Workspace() string {
	if c.workspaceSuffix == "" {
		return filepath.Join(c.base, fmt.Sprintf("%s_%s", c.Namespace, c.ReleaseName))
	} else {
		return filepath.Join(c.base, fmt.Sprintf("%s_%s_%s", c.Namespace, c.ReleaseName, c.workspaceSuffix))
	}
}

type helmWrapper struct {
	Kubeconfig  string
	Namespace   string
	ReleaseName string
	ChartName   string
	// helm action Config
	helmConf *action.Configuration
	// add labels to helm chart
	labels map[string]string
	// add annotations to helm chart
	annotations     map[string]string
	base            string
	workspaceSuffix string
	dryRun          bool
}

// The dir where chart saved
func (c *helmWrapper) chartDir() string {
	return filepath.Join(c.Workspace(), "chart")
}

func (c *helmWrapper) chartPath() string {
	return filepath.Join(c.chartDir(), fmt.Sprintf("%s.tgz", c.ChartName))
}

func (c *helmWrapper) cleanup() {
	if err := os.RemoveAll(c.Workspace()); err != nil {
		klog.Errorf("remove dir %s failed, error: %s", c.Workspace(), err)
	}
}

type Option func(*helmWrapper)

func (c *helmWrapper) Set(options ...Option) {
	for _, option := range options {
		option(c)
	}
}

func SetDryRun(dryRun bool) Option {
	return func(wrapper *helmWrapper) {
		wrapper.dryRun = dryRun
	}
}

// extra annotations added to all resources in chart
func SetAnnotations(annotations map[string]string) Option {
	return func(wrapper *helmWrapper) {
		wrapper.annotations = annotations
	}
}

// extra labels added to all resources in chart
func SetLabels(labels map[string]string) Option {
	return func(wrapper *helmWrapper) {
		wrapper.labels = labels
	}
}

// ensureWorkspace check whether workspace exists or not.
// If not exists, create workspace dir.
func (c *helmWrapper) ensureWorkspace() error {
	if exists, err := kpath.Exists(kpath.CheckFollowSymlink, c.Workspace()); err != nil {
		klog.Errorf("check dir %s failed, error: %s", c.Workspace(), err)
		return err
	} else if !exists {
		err = os.MkdirAll(c.Workspace(), os.ModeDir|os.ModePerm)
		if err != nil {
			klog.Errorf("mkdir %s failed, error: %s", c.Workspace(), err)
			return err
		}
	}

	err := os.MkdirAll(c.chartDir(), os.ModeDir|os.ModePerm)
	if err != nil {
		klog.Errorf("mkdir %s failed, error: %s", c.chartDir(), err)
		return err
	}

	return nil
}

// create chart dir in workspace
// write values.yaml into workspace
func (c *helmWrapper) createChart(chartData, values []byte, chartName string) error {
	c.ChartName = chartName

	// write chart
	f, err := os.Create(c.chartPath())

	if err != nil {
		return err
	}

	_, err = f.Write(chartData)

	if err != nil {
		return err
	}
	f.Close()

	// write values
	f, err = os.Create(filepath.Join(c.Workspace(), "values.yaml"))
	if err != nil {
		return err
	}

	_, err = f.Write(values)
	if err != nil {
		return err
	}

	f.Close()
	return nil
}

// helm uninstall
func (c *helmWrapper) Uninstall() error {
	start := time.Now()
	defer func() {
		klog.V(2).Infof("run command end, namespace: %s, name: %s elapsed: %v", c.Namespace, c.ReleaseName, time.Since(start))
	}()

	uninstall := action.NewUninstall(c.helmConf)
	if c.dryRun {
		uninstall.DryRun = true
	}

	_, err := uninstall.Run(c.ReleaseName)
	if err != nil {
		// release does not exist. It's ok.
		if fmt.Sprintf(UninstallNotFoundFormat, c.ReleaseName) == err.Error() {
			return nil
		}
		klog.Errorf("run command failed, error: %v", err)
		return err
	} else {
		klog.V(2).Infof("namespace: %s, name: %s, run command success", c.Namespace, c.ReleaseName)
	}

	return nil
}

// helm upgrade
func (c *helmWrapper) Upgrade(chartData, values []byte, chartName string) error {
	sts, err := c.Status()
	if err != nil {
		return err
	}

	if sts.Info.Status == "deployed" {
		return c.writeAction(chartData, values, chartName, true)
	} else {
		err = errors.New("cannot upgrade release current state is " + sts.Info.Status.String())
		return err
	}
}

// helm install
func (c *helmWrapper) Install(chartData, values []byte, chartName string) error {
	sts, err := c.Status()
	if err == nil {
		// helm release has been installed
		if sts.Info != nil && sts.Info.Status == "deployed" {
			return nil
		}
		return errors.New(releaseExists)
	} else {
		if err.Error() == StatusNotFoundFormat {
			// continue to install
			return c.writeAction(chartData, values, chartName, false)
		}
		return err
	}
}

// helm install
func (c *helmWrapper) InstallOrUpgrade(chartData, values []byte, chartName string) error {
	sts, err := c.Status()
	if err == nil {
		if sts.Info != nil && sts.Info.Status == "deployed" {
			return c.writeAction(chartData, values, chartName, true)
		}
	} else {
		if err.Error() == StatusNotFoundFormat {
			return c.writeAction(chartData, values, chartName, false)
		}
	}
	return nil
}

func (c *helmWrapper) helmUpgrade(chart *chart.Chart, values map[string]interface{}) (*helmrelease.Release, error) {
	upgrade := action.NewUpgrade(c.helmConf)
	upgrade.Namespace = c.Namespace
	upgrade.MaxHistory = 3

	if c.dryRun {
		upgrade.DryRun = true
	}
	if len(c.labels) > 0 || len(c.annotations) > 0 {
		postRenderer := newPostRendererKustomize(c.labels, c.annotations)
		upgrade.PostRenderer = postRenderer
	}

	return upgrade.Run(c.ReleaseName, chart, values)
}

func (c *helmWrapper) helmInstall(chart *chart.Chart, values map[string]interface{}) (*helmrelease.Release, error) {
	install := action.NewInstall(c.helmConf)
	install.ReleaseName = c.ReleaseName
	install.Namespace = c.Namespace

	if c.dryRun {
		install.DryRun = true
	}
	if len(c.labels) > 0 || len(c.annotations) > 0 {
		postRenderer := newPostRendererKustomize(c.labels, c.annotations)
		install.PostRenderer = postRenderer
	}

	return install.Run(chart, values)
}

func (c *helmWrapper) writeAction(chartData, values []byte, chartName string, upgrade bool) error {
	if klog.V(2).Enabled() {
		start := time.Now()
		defer func() {
			klog.V(2).Infof("run command end, namespace: %s, name: %s, upgrade: %t, elapsed: %v", c.Namespace, c.ReleaseName, upgrade, time.Since(start))
		}()
	}

	if err := c.ensureWorkspace(); err != nil {
		return err
	}
	defer c.cleanup()

	if err := c.createChart(chartData, values, chartName); err != nil {
		return err
	}
	klog.V(8).Infof("namespace: %s, name: %s, chart values: %s", c.Namespace, c.ReleaseName, values)

	chartRequested, err := loader.Load(c.chartPath())
	if err != nil {
		return err
	}
	valuePath := filepath.Join(c.Workspace(), "values.yaml")
	helmValues, err := chartutil.ReadValuesFile(valuePath)
	if err != nil {
		return err
	}

	var rel *helmrelease.Release
	if upgrade {
		rel, err = c.helmUpgrade(chartRequested, helmValues.AsMap())
	} else {
		rel, err = c.helmInstall(chartRequested, helmValues.AsMap())
	}

	if err != nil {
		klog.Errorf("namespace: %s, name: %s,  error: %v", c.Namespace, c.ReleaseName, err)
		return err
	}

	klog.V(2).Infof("namespace: %s, name: %s, run command success", c.Namespace, c.ReleaseName)
	klog.V(8).Infof("namespace: %s, name: %s, run command success, manifest: %s", c.Namespace, c.ReleaseName, rel.Manifest)
	return nil
}

func (c *helmWrapper) Manifest() (string, error) {
	get := action.NewGet(c.helmConf)

	rel, err := get.Run(c.ReleaseName)

	if err != nil {
		klog.Errorf("namespace: %s, name: %s, run command failed, error: %v", c.Namespace, c.ReleaseName, err)
		return "", err
	}
	klog.V(2).Infof("namespace: %s, name: %s, run command success", c.Namespace, c.ReleaseName)
	klog.V(8).Infof("namespace: %s, name: %s, run command success, manifest: %s", c.Namespace, c.ReleaseName, rel.Manifest)
	return rel.Manifest, nil
}
