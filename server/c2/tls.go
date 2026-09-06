package c2

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
)

// tlsConfigFrom builds a TLS config for an HTTPS listener from its options JSONB.
// Supported keys (PEM inline or file paths):
//
//	{"tls_cert": "...", "tls_key": "..."}
//	{"tls_cert_file": "/path/cert.pem", "tls_key_file": "/path/key.pem"}
func tlsConfigFrom(options json.RawMessage) (*tls.Config, error) {
	var o struct {
		TLSCert     string `json:"tls_cert"`
		TLSKey      string `json:"tls_key"`
		TLSCertFile string `json:"tls_cert_file"`
		TLSKeyFile  string `json:"tls_key_file"`
	}
	if len(options) > 0 {
		_ = json.Unmarshal(options, &o)
	}
	var cert tls.Certificate
	var err error
	switch {
	case o.TLSCert != "" && o.TLSKey != "":
		cert, err = tls.X509KeyPair([]byte(o.TLSCert), []byte(o.TLSKey))
	case o.TLSCertFile != "" && o.TLSKeyFile != "":
		cert, err = tls.LoadX509KeyPair(o.TLSCertFile, o.TLSKeyFile)
	default:
		return nil, fmt.Errorf("HTTPS 需要 options.tls_cert/tls_key 或 tls_cert_file/tls_key_file")
	}
	if err != nil {
		return nil, fmt.Errorf("加载 TLS 证书失败: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}
