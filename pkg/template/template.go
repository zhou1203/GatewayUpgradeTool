package template

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"

	gatewayv2alpha1 "github.com/zhou1203/GatewayUpgradeTool/api/gateway/v2alpha1"
)

const (
	AnnotationsNodePortHttp  = "gateway.kubesphere.io/nodeport-http"
	AnnotationsNodePortHttps = "gateway.kubesphere.io/nodeport-https"
)

//go:embed values.yaml
var fs embed.FS

type GatewayTemplate struct {
	FullnameOverride string `yaml:"fullnameOverride"`

	Controller Controller `yaml:"controller"`
}

type Controller struct {
	Image                Image                `yaml:"image"`
	IngressClassResource IngressClassResource `yaml:"ingressClassResource"`
	ReplicaCount         int                  `yaml:"replicaCount,omitempty"`
	Annotations          map[string]string    `yaml:"annotations,omitempty"`
	Config               map[string]string    `yaml:"config,omitempty"`
	Service              Service              `yaml:"service,omitempty"`
	Resources            Resource             `yaml:"resources,omitempty"`
	IntegrateKubeSphere  Integrate            `yaml:"integrateKubeSphere,omitempty"`
}

type IngressClassResource struct {
	Name string `yaml:"name"`
}

type Integrate struct {
	Tracing bool  `yaml:"tracing,omitempty"`
	Scope   Scope `yaml:"scope,omitempty"`
}

type Resource struct {
	Requests map[string]string `yaml:"requests,omitempty"`
	Limits   map[string]string `yaml:"limits,omitempty"`
}

type Image struct {
	Repository string `yaml:"repository"`
}

type Scope struct {
	Enabled           bool   `yaml:"enabled,omitempty"`
	Namespace         string `yaml:"namespace,omitempty"`
	NamespaceSelector string `yaml:"namespaceSelector,omitempty"`
}
type Service struct {
	Annotations map[string]string `yaml:"annotations,omitempty"`
	Type        string            `yaml:"type,omitempty"`
	NodePorts   NodePorts         `yaml:"nodePorts,omitempty"`
}

type NodePorts struct {
	Http  string `yaml:"http,omitempty"`
	Https string `yaml:"https,omitempty"`
}

func HandleTemplate(gw *gatewayv2alpha1.Gateway) ([]byte, error) {
	tmplName := "values.yaml"

	gatewaySpec, err := fromGatewayValues(gw.Spec.Values.Raw)
	if err != nil {
		return nil, err
	}

	if gw.Annotations != nil {
		gatewaySpec.Controller.Service.NodePorts.Http = gw.Annotations[AnnotationsNodePortHttp]
		gatewaySpec.Controller.Service.NodePorts.Https = gw.Annotations[AnnotationsNodePortHttps]
	}
	tmpl, err := template.New(tmplName).Funcs(template.FuncMap{
		"toYaml":  toYaml,
		"nindent": nindent,
	}).ParseFS(fs, tmplName)
	if err != nil {
		klog.Errorf("failed to parse template: %v", err)
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, tmplName, gatewaySpec); err != nil {
		klog.Errorf("failed to execute template: %v", err)
		return nil, err
	}

	var values interface{}
	if err := yaml.Unmarshal(buf.Bytes(), &values); err != nil {
		klog.Errorf("failed to unmarshal: %v", err)
		return nil, err
	}
	values = convert(values)
	jsonBytes, err := json.MarshalIndent(values, "", "  ")
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

// FromGatewaySpec populates all available fields in GatewayTemplate from the parsed YAML values.
func fromGatewayValues(values []byte) (*GatewayTemplate, error) {
	t := &GatewayTemplate{}
	err := yaml.Unmarshal(values, t)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func convert(v interface{}) interface{} {
	switch v := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{})
		for key, value := range v {
			m[fmt.Sprint(key)] = convert(value)
		}
		return m
	case []interface{}:
		for i, value := range v {
			v[i] = convert(value)
		}
	}
	return v
}
