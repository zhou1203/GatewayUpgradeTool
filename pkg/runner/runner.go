package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zhou1203/GatewayUpgradeTool/cmd/gatewayupgradetool/options"
	"sigs.k8s.io/yaml"

	gatewayv2alpha1 "github.com/zhou1203/GatewayUpgradeTool/api/gateway/v2alpha1"
	"github.com/zhou1203/GatewayUpgradeTool/pkg/scheme"
	"github.com/zhou1203/GatewayUpgradeTool/pkg/simple/helmwrapper"
	"github.com/zhou1203/GatewayUpgradeTool/pkg/template"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	TargetVersion = "kubesphere-nginx-ingress-4.12.1"
)

type Runner struct {
	Client       client.Client
	GatewayNames []string
	AppVersion   string
	KubeConfig   string

	NeedBackup bool
	BackupDir  string
}

func NewRunner(options *options.RunOptions) (*Runner, error) {
	r := &Runner{}
	if options.KubeConfig == "" {
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		options.KubeConfig = config.String()
	}
	kubeClient, err := buildClient(options.KubeConfig)
	if err != nil {
		return nil, err
	}
	r.Client = kubeClient
	r.AppVersion = options.AppVersion
	r.GatewayNames = strings.Split(options.GatewayNames, ",")
	r.NeedBackup = options.NeedBackup
	r.BackupDir = options.BackupDir
	return r, nil
}

func (r *Runner) Run(ctx context.Context) {
	err := r.CheckAndCompleteGateways(ctx)
	if err != nil {
		klog.Fatal("failed to check gateways", err)
		return
	}

	gateways, err := r.getGateways(ctx)
	if err != nil {
		klog.Fatal("failed to get gateways", err)
		return
	}

	if r.NeedBackup {
		err := r.CreateBackup(gateways)
		if err != nil {
			klog.Fatal("failed to create backup", err)
			return
		}
	}

	err = r.UpgradeGateways(ctx, gateways)
	if err != nil {
		klog.Fatal("failed to upgrade gateways", err)
		return
	}

}

func (r *Runner) getGateways(ctx context.Context) ([]gatewayv2alpha1.Gateway, error) {
	var list []gatewayv2alpha1.Gateway
	for _, fullName := range r.GatewayNames {
		split := strings.Split(fullName, ":")
		if len(split) != 2 {
			return nil, fmt.Errorf("invalid gateway name: %s", fullName)
		}
		namespace := split[0]
		name := split[1]
		gateway := &gatewayv2alpha1.Gateway{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, gateway)
		if err != nil {
			return nil, err
		}
		gateway.Kind = "Gateway"
		gateway.APIVersion = gatewayv2alpha1.SchemeGroupVersion.String()
		list = append(list, *gateway)
	}
	return list, nil
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
			if gw.Spec.AppVersion != TargetVersion {
				r.GatewayNames = append(r.GatewayNames, fmt.Sprintf("%s:%s", gw.Namespace, gw.Name))
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
			}
			r.GatewayNames = append(r.GatewayNames, fullName)
		}
	}
	return nil
}

func (r *Runner) UpgradeGateways(ctx context.Context, gateways []gatewayv2alpha1.Gateway) error {
	for _, gw := range gateways {
		err := r.upgrade(ctx, gw)
		if err != nil {
			return fmt.Errorf("failed to upgrade gateway %s: %v", gw.Name, err)
		}
	}
	return nil
}

func (r *Runner) upgrade(ctx context.Context, old gatewayv2alpha1.Gateway) error {
	service := &corev1.Service{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: old.Namespace, Name: old.Name}, service)
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

	jsonBytes, err := template.TemplateHandler(&old)
	if err != nil {
		return err
	}

	waitReleaseFunc := func() error {
		time.Sleep(5 * time.Second)
		wrapper := helmwrapper.NewHelmWrapper(r.KubeConfig, old.Namespace, old.Name)
		ready, err := wrapper.IsReleaseReady(5 * time.Minute)
		if err != nil {
			return err
		}
		if !ready {
			return fmt.Errorf("gateway '%s:%s' is not ready, wait for release timeout", old.Namespace, old.Name)
		}
		return nil
	}

	deepCopy := old.DeepCopy()
	deepCopy.Spec.AppVersion = TargetVersion
	deepCopy.Spec.Values = runtime.RawExtension{Raw: jsonBytes}
	err = r.Client.Update(ctx, deepCopy)
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

func (r *Runner) CreateBackup(gateways []gatewayv2alpha1.Gateway) error {

	fullPath := filepath.Join(r.BackupDir, fmt.Sprintf("gateway-backup-%s.yaml", time.Now().Format("20060102150405")))

	err := os.MkdirAll(r.BackupDir, 0755)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(fullPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	defer file.Close()

	for _, gateway := range gateways {
		marshal, err := yaml.Marshal(gateway)
		if err != nil {
			return err
		}
		_, err = file.Write(marshal)
		if err != nil {
			return err
		}
		_, err = file.WriteString("\n---\n")
		if err != nil {
			return err
		}
	}

	return nil
}
