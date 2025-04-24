package scheme

import (
	gatewayv2alpha1 "github.com/zhou1203/GatewayUpgrader/api/gateway/v2alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// Scheme contains all types of custom Scheme and kubernetes client-go Scheme.
var Scheme = runtime.NewScheme()

func init() {
	// register common meta types into schemas.
	metav1.AddToGroupVersion(Scheme, metav1.SchemeGroupVersion)

	_ = clientgoscheme.AddToScheme(Scheme)

	utilruntime.Must(gatewayv2alpha1.AddToScheme(Scheme))
}
