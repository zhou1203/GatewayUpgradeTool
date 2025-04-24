package runner

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zhou1203/GatewayUpgrader/cmd/upgrade/options"

	gatewayv2alpha1 "github.com/zhou1203/GatewayUpgrader/api/gateway/v2alpha1"
	"github.com/zhou1203/GatewayUpgrader/pkg/scheme"
	"github.com/zhou1203/GatewayUpgrader/pkg/simple/helmwrapper"
	"github.com/zhou1203/GatewayUpgrader/pkg/template"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Runner struct {
	Client        client.Client
	GatewayNames  []string
	AppVersion    string
	TargetVersion string
	TemplateFile  string
	KubeConfig    string
}

func NewRunner(options *options.RunOptions) (*Runner, error) {
	r := &Runner{}

	kubeClient, err := buildClient(options.KubeConfig)
	if err != nil {
		return nil, err
	}
	r.Client = kubeClient
	r.AppVersion = options.AppVersion
	r.TargetVersion = options.TargetVersion
	r.TemplateFile = options.TemplateFile
	r.GatewayNames = strings.Split(options.GatewayNames, ",")
	return r, nil
}

func (r *Runner) Run(ctx context.Context) {
	err := r.CheckAndCompleteGateways(ctx)
	if err != nil {
		klog.Error("failed to check gateways", err)
		return
	}
	for _, fullName := range r.GatewayNames {
		err := r.upgradeGateway(ctx, fullName)
		if err != nil {
			klog.Errorf("Failed to upgrade gateway '%s': %v", fullName, err)
			return
		}
		klog.Infof("Successfully upgrade gateway '%s'", fullName)
	}
}

func (r *Runner) CheckAndCompleteGateways(ctx context.Context) error {
	noChecked := r.GatewayNames
	r.GatewayNames = []string{}
	if slices.Contains(noChecked, "*") {
		list := &gatewayv2alpha1.GatewayList{}
		err := r.Client.List(ctx, list)
		if err != nil {
			return err
		}
		for _, gw := range list.Items {
			if gw.Spec.AppVersion == r.TargetVersion {
				r.GatewayNames = append(r.GatewayNames, gw.Name)
			}
		}
	} else {
		for _, rn := range noChecked {
			gwNs := "kubesphere-controls-system"
			gwName := rn
			fullName := ""
			gw := &gatewayv2alpha1.Gateway{}
			split := strings.Split(rn, ":")
			if len(split) > 1 {
				gwNs = split[0]
				gwName = split[1]

			}
			fullName = gwNs + ":" + gwName
			err := r.Client.Get(ctx, types.NamespacedName{Namespace: gwNs, Name: gwName}, gw)
			if err != nil {
				return err
			}
			if r.AppVersion != "" && gw.Spec.AppVersion != r.AppVersion {
				klog.Warningf("invalid gateway %s: app version does not match, will skip it", fullName)
				continue
			} else if gw.Spec.AppVersion == r.TargetVersion {
				klog.Warningf("invalid gateway %s, no need to upgrade, will skip it", fullName)
				continue
			}
			r.GatewayNames = append(r.GatewayNames, fullName)
		}
	}
	return nil
}

func (r *Runner) upgradeGateway(ctx context.Context, fullName string) error {
	split := strings.Split(fullName, ":")
	if len(split) != 2 {
		return fmt.Errorf("invalid gateway name: %s", fullName)
	}
	namespace := split[0]
	name := split[1]
	old := &gatewayv2alpha1.Gateway{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, old)
	if err != nil {
		return err
	}
	service := &corev1.Service{}
	err = r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, service)
	if err != nil {
		return err
	}
	if service.Spec.Type == corev1.ServiceTypeNodePort {
		if old.Annotations == nil {
			old.Annotations = map[string]string{}
		}
		for _, port := range service.Spec.Ports {
			if port.Name == "http" {
				old.Annotations[template.AnnotationsNodePortHttp] = strconv.Itoa(int(port.NodePort))
			}
			if port.Name == "https" {
				old.Annotations[template.AnnotationsNodePortHttps] = strconv.Itoa(int(port.NodePort))
			}
		}
	}

	jsonBytes, err := template.TemplateHandler(old, r.TemplateFile)
	if err != nil {
		return err
	}

	waitReleaseFunc := func() error {
		wrapper := helmwrapper.NewHelmWrapper(r.KubeConfig, namespace, name)
		ready, err := wrapper.IsReleaseReady(5 * time.Minute)
		if err != nil {
			return err
		}
		if !ready {
			return fmt.Errorf("gateway %s is not ready, wait for release timeout", fullName)
		}
		return nil
	}

	deepCopy := old.DeepCopy()
	deepCopy.Spec.AppVersion = r.TargetVersion
	deepCopy.Spec.Values = runtime.RawExtension{Raw: jsonBytes}
	err = r.Client.Delete(ctx, old)
	if err != nil {
		return err
	}
	err = waitReleaseFunc()
	if err != nil {
		return err
	}
	err = r.Client.Create(ctx, deepCopy)
	if err != nil {
		return err
	}
	err = waitReleaseFunc()
	if err != nil {
		return err
	}

	return nil
}

func buildClient(kubeconfig string) (client.Client, error) {
	var config *rest.Config
	var err error
	if kubeconfig == "" {
		config, err = rest.InClusterConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		return nil, err
	}

	return client.New(config, client.Options{Scheme: scheme.Scheme})
}
