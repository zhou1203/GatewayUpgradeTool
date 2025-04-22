package pkg

import (
	"bytes"
	"embed"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
	"strings"
	"text/template"

	gatewayv2alpha1 "github.com/zhou1203/GatewayUpgrader/api/gateway/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

//go:embed values.yaml
var fs embed.FS

type GatewayTemplate struct {
	Deployment DeploymentSpec
	Service    ServiceSpec
	Controller ControllerSpec
}

type DeploymentSpec struct {
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// +optional
	Affinity corev1.Affinity `json:"affinity,omitempty"`
}

type ControllerSpec struct {
	FullnameOverride string `json:"-"`
	Repository       string `json:"-"`
	// Deprecated: Use Deployment Replicas instead
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// Deprecated: Use Deployment Annotations instead
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Config map[string]string `json:"config,omitempty"`
	// +optional
	Scope Scope `json:"scope,omitempty"`
	// +optional
	TCP map[string]string `json:"tcp,omitempty"`
	// +optional
	UDP map[string]string `json:"udp,omitempty"`
}

type Scope struct {
	Enabled           bool   `json:"enabled,omitempty"`
	Namespace         string `json:"namespace,omitempty"`
	NamespaceSelector string `json:"namespaceSelector,omitempty"`
}
type ServiceSpec struct {
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Type string `json:"type,omitempty"`
}

func TemplateHandler(spec *gatewayv2alpha1.GatewaySpec) ([]byte, error) {
	tmplName := "values.yaml"

	tmpl, err := template.New(tmplName).Funcs(template.FuncMap{
		"toYaml":  toYaml,
		"nindent": nindent,
	}).ParseFS(fs, tmplName)
	if err != nil {
		klog.Errorf("failed to parse template: %v", err)
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, tmplName, spec); err != nil {
		klog.Errorf("failed to execute template: %v", err)
		return nil, err
	}

	values := map[string]any{}
	if err := yaml.Unmarshal(buf.Bytes(), &values); err != nil {
		klog.Errorf("failed to unmarshal: %v", err)
		return nil, err
	}

	jsonBytes, err := json.Marshal(values)
	if err != nil {
		klog.Errorf("failed to marshal: %v", err)
		return nil, err
	}

	return jsonBytes, nil
}

func toYaml(data any) (string, error) {
	// There may be fields in data that do not need to be serialized (marked by json tags),
	// so they cannot be directly serialized into yaml. They need to be serialized into json first and finally into yaml.
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	var unmarshaledData any
	if err := json.Unmarshal(jsonBytes, &unmarshaledData); err != nil {
		return "", err
	}

	yamlBytes, err := yaml.Marshal(unmarshaledData)
	if err != nil {
		return "", err
	}

	ret := string(yamlBytes)
	if strings.Contains(ret, "null") {
		ret = strings.ReplaceAll(ret, "null", "{}")
	}
	return ret, nil
}

func nindent(indent int, value string) string {
	blank, line := " ", "\n"
	indentedValue := strings.ReplaceAll(value, line, line+strings.Repeat(blank, indent))
	return line + strings.Repeat(blank, indent) + indentedValue
}
