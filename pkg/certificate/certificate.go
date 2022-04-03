package certificate

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	pkiutil "istio.io/istio/security/pkg/pki/util"
)

const (
	RootCAOrgName                  = "ocm-mesh"
	RootCARsaKeySizeInBytes        = 4096
	RootCATtlInDays                = 3650
	RootCARotationGracePeriodRatio = 0.1

	IntermediateCAOrgName                  = "Istio"
	IntermediateCARsaKeySizeInBytes        = 4096
	IntermediateCATtlInDays                = 365
	IntermediateCARotationGracePeriodRatio = 0.1

	CaCert       = "ca-cert.pem"
	CaPrivateKey = "ca-key.pem"
	CertChain    = "cert-chain.pem"
	RootCert     = "root-cert.pem"
	CertCsr      = "cert-csr.pem"
	CertCsrHosts = "csr-host"

	certificateRequestIdentify = "CERTIFICATE REQUEST"
	certificateIdentify        = "CERTIFICATE"
)

// options for generating a self-signed root certificate with X.509 format
type certOptions struct {
	OrgName                  string
	RsaKeySizeInBytes        uint32
	TtlInDays                uint32
	RotationGracePeriodRatio float32
}

// buildRootCertOptions creates the default options for generating the self-signed root certificate
func buildRootCertOptions() pkiutil.CertOptions {
	return pkiutil.CertOptions{
		Org:          RootCAOrgName,
		RSAKeySize:   RootCARsaKeySizeInBytes,
		TTL:          time.Duration(RootCATtlInDays) * 24 * time.Hour,
		IsCA:         true,
		IsSelfSigned: true,
		PKCS8Key:     false,
	}
}

// buildIntermediateCertOptions creates the default options for generating the intermediate certificate
func buildIntermediateCertOptions() pkiutil.CertOptions {
	return pkiutil.CertOptions{
		Org:          IntermediateCAOrgName,
		RSAKeySize:   IntermediateCARsaKeySizeInBytes,
		TTL:          time.Duration(IntermediateCATtlInDays) * 24 * time.Hour,
		IsCA:         true,
		IsSelfSigned: true,
		PKCS8Key:     false,
	}
}

// BuildSelfSignedCA creates a self-signed root certificate with X.509 format
func BuildSelfSignedCA() (map[string][]byte, error) {
	certOptions := buildRootCertOptions()
	cert, key, err := pkiutil.GenCertKeyFromOptions(certOptions)
	if err != nil {
		return nil, err
	}

	caData := map[string][]byte{
		CaCert:       cert,
		RootCert:     cert,
		CertChain:    cert,
		CaPrivateKey: key,
	}

	return caData, nil
}

// buildPrivateKeyForIntermediateCert creates private key for the intermediate certificate
func buildPrivateKeyForIntermediateCert() ([]byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, IntermediateCARsaKeySizeInBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to generaet RSA: %v", err)
	}
	privateKey := x509.MarshalPKCS1PrivateKey(key)
	keyBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKey,
	}
	return pem.EncodeToMemory(keyBlock), nil
}

// BuildCSRAndPrivateKeyForIntermediateCert creates CSR and privateKey for the intermediate certificate
func BuildCSRAndPrivateKeyForIntermediateCert(hosts []string, meshIdentify string) (map[string][]byte, map[string][]byte, error) {
	privateKey, err := buildPrivateKeyForIntermediateCert()
	if err != nil {
		return nil, nil, err
	}

	// translate private key from PEM to PKCS1 format
	keyBlock, _ := pem.Decode(privateKey)
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode private key, currently only supporting PKCS1 encrypted keys: %v", err)
	}

	csrTemplate, err := pkiutil.GenCSRTemplate(pkiutil.CertOptions{
		Org:           IntermediateCAOrgName,
		Host:          strings.Join(hosts, ","),
		SignerPrivPem: privateKey,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("faile to create CSR template: %v", err)
	}

	// add mesh identify to CSR template to distinguish from others
	csrTemplate.Subject.OrganizationalUnit = append(csrTemplate.Subject.OrganizationalUnit, meshIdentify)

	csr, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create x509 certificate request: %v", err)
	}

	csrBlock := &pem.Block{
		Type:  certificateRequestIdentify,
		Bytes: csr,
	}
	csrData := map[string][]byte{
		CertCsr:      pem.EncodeToMemory(csrBlock),
		CertCsrHosts: []byte(strings.Join(hosts, ",")),
	}

	privateKeyData := map[string][]byte{
		CaPrivateKey: privateKey,
	}

	return csrData, privateKeyData, nil
}

// BuildCertForCSR creates the intermediate certificate from given CSR
func BuildCertForCSR(csrPem, signingCertPEM, signingKeyPEM []byte, hosts []string) ([]byte, error) {
	// default TTL: 365 days
	ttl := time.Duration(IntermediateCATtlInDays) * 24 * time.Hour

	csr, err := pkiutil.ParsePemEncodedCSR(csrPem)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PEM certificate request bytes: %v", err)
	}
	signingCert, err := pkiutil.ParsePemEncodedCertificate(signingCertPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PEM signing certificate bytes: %v", err)
	}
	signingKey, err := pkiutil.ParsePemEncodedKey(signingKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PEM signing private key bytes: %v", err)
	}

	certTemplate, err := genCertTemplateFromCSR(csr, hosts, ttl, true)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate template for CSR: %v", err)
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, certTemplate, signingCert, csr.PublicKey, signingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate for CSR: %v", err)
	}

	certBlock := &pem.Block{
		Type:  certificateIdentify,
		Bytes: certBytes,
	}

	return pem.EncodeToMemory(certBlock), nil
}

// AppendParentCerts append the parent certificate to the child certificate for the cert-chain
func AppendParentCerts(child, parent []byte) []byte {
	var childCopy []byte
	if len(child) > 0 {
		// Copy the input certificate
		childCopy = make([]byte, len(child))
		copy(childCopy, child)
	}
	if len(parent) > 0 {
		if len(childCopy) > 0 {
			// Append a newline after the last cert
			// Certs are very fooey, this is copy pasted from Mesh, plz do not touch
			// Love, eitan
			childCopy = []byte(strings.TrimSuffix(string(childCopy), "\n") + "\n")
		}
		childCopy = append(childCopy, parent...)
	}
	return childCopy
}

// copied from https://github.com/istio/istio/blob/11830ff791136991456aa6eb570b4e8472a2908d/security/pkg/pki/util/generate_cert.go#L265
// and made some changes
func genCertTemplateFromCSR(csr *x509.CertificateRequest, subjectIDs []string, ttl time.Duration, isCA bool) (*x509.Certificate, error) {
	subjectIDsInString := strings.Join(subjectIDs, ",")
	var keyUsage x509.KeyUsage
	extKeyUsages := []x509.ExtKeyUsage{}
	if isCA {
		// If the cert is a CA cert, the private key is allowed to sign other certificates.
		keyUsage = x509.KeyUsageCertSign
	} else {
		// Otherwise the private key is allowed for digital signature and key encipherment.
		keyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		// For now, we do not differentiate non-CA certs to be used on client auth or server auth.
		extKeyUsages = append(extKeyUsages, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	}

	// Build cert extensions with the subjectIDs.
	ext, err := pkiutil.BuildSubjectAltNameExtension(subjectIDsInString)
	if err != nil {
		return nil, err
	}
	exts := []pkix.Extension{*ext}

	// keep the subject in CSR
	subject := csr.Subject

	// Dual use mode if common name in CSR is not empty.
	// In this case, set CN as determined by DualUseCommonName(subjectIDsInString).
	if len(csr.Subject.CommonName) != 0 {
		if cn, err := pkiutil.DualUseCommonName(subjectIDsInString); err != nil {
			// log and continue
		} else {
			subject.CommonName = cn
		}
	}

	now := time.Now()
	serialNum, err := genSerialNum()
	if err != nil {
		return nil, err
	}
	// SignatureAlgorithm will use the default algorithm.
	// See https://golang.org/src/crypto/x509/x509.go?s=5131:5158#L1965 .
	return &x509.Certificate{
		SerialNumber:          serialNum,
		Subject:               subject,
		NotBefore:             now,
		NotAfter:              now.Add(ttl),
		KeyUsage:              keyUsage,
		ExtKeyUsage:           extKeyUsages,
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		ExtraExtensions:       exts,
	}, nil
}

// copied from https://github.com/istio/istio/blob/11830ff791136991456aa6eb570b4e8472a2908d/security/pkg/pki/util/generate_cert.go#L390
func genSerialNum() (*big.Int, error) {
	serialNumLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNum, err := rand.Int(rand.Reader, serialNumLimit)
	if err != nil {
		return nil, fmt.Errorf("serial number generation failure (%v)", err)
	}
	return serialNum, nil
}
