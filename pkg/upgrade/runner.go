package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dario.cat/mergo"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	gatewayv2alpha2 "github.com/zhou1203/GatewayUpgradeTool/api/gateway/v2alpha2"
	"github.com/zhou1203/GatewayUpgradeTool/cmd/upgrade/options"
	"github.com/zhou1203/GatewayUpgradeTool/pkg/scheme"
	"github.com/zhou1203/GatewayUpgradeTool/pkg/simple/helmwrapper"
	"github.com/zhou1203/GatewayUpgradeTool/pkg/template"
)

const (
	TargetVersion = "kubesphere-nginx-ingress-4.12.1"

	ExtensionNamespace   = "extension-gateway"
	GatewayConfigMapName = "gateway-agent-backend-config"
)

type Runner struct {
	Client       client.Client
	GatewayNames []*gatewayv2alpha2.GatewayReference
	Kubeconfig   []byte
	RunOptions   options.RunOptions
}

type BackupOptions struct {
	Enabled bool
	DirPath string
}

func NewRunner(options *options.RunOptions) (*Runner, error) {
	r := &Runner{}
	kubeClient, err := buildClient(options.KubeConfigPath)
	if err != nil {
		return nil, err
	}
	r.Client = kubeClient
	r.RunOptions = *options
	if GetAll(options.GatewayNames) {
		// TODO get all gateways
		klog.Warning("upgrade all gateways is not supported yet")
	}
	if r.RunOptions.KubeConfigPath != "" {
		file, err := os.ReadFile(options.KubeConfigPath)
		if err != nil {
			return nil, err
		}
		r.Kubeconfig = file
	}
	r.GatewayNames = newGatewayReferences(options.GatewayNames)
	return r, nil
}

func (r *Runner) getAllGateways(ctx context.Context) ([]gatewayv2alpha2.Gateway, error) {
	gatewayList := &gatewayv2alpha2.GatewayList{}
	err := r.Client.List(ctx, gatewayList)
	if err != nil {
		return nil, err
	}
	return gatewayList.Items, nil
}

func GetAll(options string) bool {
	return options == "*"
}

func newGatewayReferences(gatewayNames string) []*gatewayv2alpha2.GatewayReference {
	gatewayRefs := make([]*gatewayv2alpha2.GatewayReference, 0)
	split := strings.Split(gatewayNames, ",")
	for _, fullName := range split {
		gatewayRef := &gatewayv2alpha2.GatewayReference{}
		gatewayRef.FromString(fullName)
		gatewayRefs = append(gatewayRefs, gatewayRef)
	}
	return gatewayRefs
}

func (r *Runner) Run(ctx context.Context) error {
	if len(r.GatewayNames) == 0 {
		klog.Infof("No gateway need to upgrade")
		return nil
	}
	gateways, err := r.getGateways(ctx)
	if err != nil {
		return fmt.Errorf("failed to get gateways: %w", err)
	}

	gatewayFullNames := make([]string, 0, len(gateways))
	for _, g := range gateways {
		fullName := fmt.Sprintf("%s/%s", g.Namespace, g.Name)
		gatewayFullNames = append(gatewayFullNames, fullName)
	}

	if r.RunOptions.Backup.Enabled {
		klog.Info("Start to backup gateways. gateways: ", gatewayFullNames)
		err := r.CreateBackupFile(gateways)
		if err != nil {
			return fmt.Errorf("failed to backup gateways: %w", err)
		}
	}
	klog.Info("Start to upgrade gateways. gateways: ", gatewayFullNames)
	err = r.UpgradeGateways(ctx, gateways)
	if err != nil {
		return fmt.Errorf("failed to upgrade gateways: %w", err)
	}
	return nil
}

func (r *Runner) getGateways(ctx context.Context) ([]gatewayv2alpha2.Gateway, error) {
	var list []gatewayv2alpha2.Gateway
	for _, fullName := range r.GatewayNames {
		gateway := &gatewayv2alpha2.Gateway{}
		err := r.Client.Get(ctx, fullName.ToNamespacedName(), gateway)
		if err != nil {
			return nil, err
		}
		gateway.Kind = gatewayv2alpha2.GatewayKind
		gateway.APIVersion = gatewayv2alpha2.SchemeGroupVersion.String()
		list = append(list, *gateway)
	}
	return list, nil
}

func (r *Runner) isRequiredVersion(appVersion string) bool {
	return (r.RunOptions.SpecificAppVersion != "" && appVersion == r.RunOptions.SpecificAppVersion) || appVersion != TargetVersion
}

func (r *Runner) UpgradeGateways(ctx context.Context, gateways []gatewayv2alpha2.Gateway) error {
	for _, gw := range gateways {
		klog.Infof("Begin to Upgrade gateway %s/%s.", gw.Namespace, gw.Name)
		if !r.isRequiredVersion(gw.Spec.AppVersion) {
			klog.Warningf("Gateway %s app version does not match, will skip it", gw.Name)
			continue
		}
		if !gw.IsDeployed() {
			klog.Warningf("Gateway %s is not deployed, will skip it", gw.Name)
			continue
		}
		if !gw.IsDeploymentReady() {
			klog.Warningf("Gateway %s is not ready, will skip it", gw.Name)
			continue
		}
		err := r.upgrade(ctx, gw)
		if err != nil {
			return fmt.Errorf("failed to upgrade gateway %s: %v", gw.Name, err)
		}
		klog.Infof("Upgrade gateway %s/%s successfully.", gw.Namespace, gw.Name)
	}
	return nil
}

func (r *Runner) upgrade(ctx context.Context, old gatewayv2alpha2.Gateway) error {
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

	jsonBytes, err := template.HandleTemplate(&old)
	if err != nil {
		return err
	}

	waitReleaseFunc := func() error {
		time.Sleep(5 * time.Second)
		wrapper := helmwrapper.NewHelmWrapper(string(r.Kubeconfig), old.Namespace, old.Name)
		ready, err := wrapper.IsReleaseReady(5 * time.Minute)
		if err != nil {
			return err
		}
		if !ready {
			return fmt.Errorf("gateway '%s/%s' is not ready, wait for release timeout", old.Namespace, old.Name)
		}
		return nil
	}

	values, err := r.valueOverride(ctx, jsonBytes)
	if err != nil {
		return err
	}

	deepCopy := old.DeepCopy()
	deepCopy.Spec.AppVersion = TargetVersion
	deepCopy.Spec.Values = runtime.RawExtension{Raw: values}

	ingressClassList := &v1.IngressClassList{}
	err = r.Client.List(ctx, ingressClassList, client.MatchingLabels{"app.kubernetes.io/instance": old.Name})
	if err != nil {
		return err
	}
	if len(ingressClassList.Items) == 0 {
		return fmt.Errorf("get gateway: %s ingressClass failed, please check it", old.Name)
	}
	oldIngressClassName := ingressClassList.Items[0].Name

	err = r.Client.Delete(ctx, &v1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: oldIngressClassName}})
	if err != nil {
		return err
	}
	klog.Infof("Delete old ingress class %s successfully.", oldIngressClassName)
	err = r.Client.Update(ctx, deepCopy)
	if err != nil {
		return err
	}
	err = waitReleaseFunc()
	if err != nil {
		return err
	}
	klog.Infof("Update gateway CR successfully, gateway: %s/%s", old.Namespace, old.Name)

	return nil
}

func (r *Runner) valueOverride(ctx context.Context, values []byte) ([]byte, error) {
	valuesMap := map[string]interface{}{}
	err := json.Unmarshal(values, &valuesMap)
	if err != nil {
		return nil, err
	}

	gatewayCm := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{Namespace: ExtensionNamespace, Name: GatewayConfigMapName}, gatewayCm)
	if err != nil {
		return nil, err
	}

	overrideOptions := &GatewayConfig{}
	config := gatewayCm.Data["config.yaml"]
	err = yaml.Unmarshal([]byte(config), overrideOptions)
	if err != nil {
		return nil, err
	}

	err = mergo.Map(&valuesMap, overrideOptions.Gateway.ValuesOverride, mergo.WithOverride)
	if err != nil {
		return nil, err
	}
	marshal, err := json.Marshal(valuesMap)
	if err != nil {
		return nil, err
	}
	return marshal, nil
}

type OverrideOptions struct {
	Namespace      string                 `yaml:"namespace"`
	ValuesOverride map[string]interface{} `yaml:"valuesOverride"`
}
type GatewayConfig struct {
	Gateway OverrideOptions `yaml:"gateway"`
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

func (r *Runner) CreateBackupFile(gateways []gatewayv2alpha2.Gateway) error {
	backupDir := r.RunOptions.Backup.Dir
	fullPath := filepath.Join(backupDir, fmt.Sprintf("gateway-backup-%s.yaml", time.Now().Format("20060102150405")))

	err := os.MkdirAll(backupDir, 0755)
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
		klog.Info("Backup gateway successfully. gateway: ", fmt.Sprintf(gateway.Name))
	}

	return nil
}
