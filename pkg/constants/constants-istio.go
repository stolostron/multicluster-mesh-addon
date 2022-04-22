package constants

// common istio constants
const (
	IstioCAConfigmapName  = "istio-ca-root-cert"
	IstioCAConfigmapKey   = "root-cert.pem"
	IstioCAConfigmapLabel = "istio.io/config"
	IstioCASecretName     = "cacerts"
)
