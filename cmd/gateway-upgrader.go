package cmd

import (
	"context"
	"flag"
	"fmt"
	gatewayv2alpha1 "github.com/zhou1203/GatewayUpgrader/api/gateway/v2alpha1"
	"github.com/zhou1203/GatewayUpgrader/pkg"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// 封装参数的结构体
type UpgradeRunOptions struct {
	Kubeconfig    string
	GatewayNames  string
	AppVersion    string
	TargetVersion string
}

type Runner struct {
	Client        client.Client
	GatewayNames  []string
	TargetVersion string
}

func main() {
	opts := parseFlags()

	runner, err := NewRunner(opts)
	if err != nil {
		klog.Fatal(err)
		return
	}
	runner.Run(signals.SetupSignalHandler())
}

func NewRunner(options *UpgradeRunOptions) (*Runner, error) {
	runner := &Runner{}

	kubeClient, err := buildKubeClient(options.Kubeconfig)
	if err != nil {
		return nil, err
	}
	runner.Client = kubeClient
	if options.GatewayNames == "*" {
		list := &gatewayv2alpha1.GatewayList{}
		err := runner.Client.List(context.Background(), list)
		if err != nil {
			return nil, err
		}
		for _, gw := range list.Items {
			if gw.Spec.AppVersion == options.AppVersion {
				runner.GatewayNames = append(runner.GatewayNames, gw.Name)
			}
		}
	} else {
		gatewayRawNames := strings.Split(options.GatewayNames, ",")
		for _, rn := range gatewayRawNames {
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
			err := runner.Client.Get(context.Background(), types.NamespacedName{Namespace: gwNs, Name: gwName}, gw)
			if err != nil {
				return nil, err
			}
			if gw.Spec.AppVersion != options.AppVersion {
				klog.Warning("invalid gateway spec: app version does not match")
				continue
			}
			runner.GatewayNames = append(runner.GatewayNames, fullName)
		}
	}
	return runner, nil
}

// 将命令行参数解析到结构体中
func parseFlags() *UpgradeRunOptions {
	opts := &UpgradeRunOptions{}

	flag.StringVar(&opts.Kubeconfig, "kubeconfig", "", "Path to the kubeconfig file (ignored if --in-cluster is set)")
	flag.StringVar(&opts.GatewayNames, "gateways", "", "Comma-separated list of gateway names to upgrade")
	flag.StringVar(&opts.AppVersion, "app-version", "", "App version")
	flag.StringVar(&opts.TargetVersion, "target-version", "", "Target version")
	flag.Parse()

	return opts
}

// 构建 Kubernetes 客户端
func buildKubeClient(kubeconfig string) (client.Client, error) {
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

	return client.New(config, client.Options{})
}

func (r *Runner) Run(ctx context.Context) {
	for _, fullName := range r.GatewayNames {
		err := r.upgradeGateway(ctx, fullName)
		if err != nil {
			klog.Error("Failed to upgrade gateway '%s': %v", fullName, err)
		}
		klog.Infof("Successfully upgrade gateway '%s'", fullName)
	}
}

// 模拟执行升级
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

	jsonBytes, err := pkg.TemplateHandler(&old.Spec)
	if err != nil {
		return err
	}
	deepCopy := old.DeepCopy()
	deepCopy.Spec.AppVersion = r.TargetVersion
	deepCopy.Spec.Values = runtime.RawExtension{Raw: jsonBytes}
	return r.Client.Update(ctx, deepCopy)
}
