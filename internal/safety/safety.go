package safety

const ToggleLabel = "kubetoggle.io/toggleable"

var protectedNamespaces = map[string]struct{}{
	"argocd":               {},
	"kube-system":          {},
	"kube-public":          {},
	"kube-node-lease":      {},
	"cert-manager":         {},
	"cloudflared":          {},
	"metallb-system":       {},
	"envoy-gateway-system": {},
	"gateway-system":       {},
	"kong-system":          {},
	"traefik":              {},
	"kubetoggle":           {},
}

// IsEligible reports whether a Deployment is explicitly opted in
// and is not located in a protected namespace.
func IsEligible(namespace string, labels map[string]string) bool {
	if _, protected := protectedNamespaces[namespace]; protected {
		return false
	}

	return labels[ToggleLabel] == "true"
}
