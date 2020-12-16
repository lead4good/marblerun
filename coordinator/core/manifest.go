// Copyright (c) Edgeless Systems GmbH.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package core

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"text/template"

	"github.com/edgelesssys/marblerun/coordinator/quote"
	"github.com/edgelesssys/marblerun/coordinator/rpc"
)

// Manifest defines the rules of a mesh.
type Manifest struct {
	// Packages contains the allowed enclaves and their properties.
	Packages map[string]quote.PackageProperties
	// Infrastructures contains the allowed infrastructure providers and their properties.
	Infrastructures map[string]quote.InfrastructureProperties
	// Marbles contains the allowed services with their corresponding enclave and configuration parameters.
	Marbles map[string]Marble
	// Clients contains TLS certificates for authenticating clients that use the ClientAPI.
	Clients map[string][]byte
	// Secrets holds user-specified secrets, which should be generated and later on stored in a marble (if not shared) or in the core (if shared).
	Secrets map[string]Secret
	// Recovery holds a RSA public key to encrypt the state encryption key, which gets returned over the Client API when setting a manifest.
	RecoveryKey string
}

// Marble describes a service in the mesh that should be handled and verified by the Coordinator
type Marble struct {
	// Package references one of the allowed enclaves in the manifest.
	Package string
	// MaxActivations allows to limit the number of marbles of a kind.
	MaxActivations uint
	// Parameters contains lists for files, environment variables and commandline arguments that should be passed to the application.
	// Placeholder variables are supported for specific assets of the marble's activation process.
	Parameters *rpc.Parameters
}

// Secret describes a structure for storing certificates and keys, which can be used in combination with the go templating engine.

// PrivateKey is a wrapper for a binary private key, which we need for type differentiation in the PEM encoding function
type PrivateKey []byte

// PublicKey is a wrapper for a binary public key, which we need for type differentiation in the PEM encoding function
type PublicKey []byte

// Secret defines a structure for storing certificates & encryption keys
type Secret struct {
	Type     string
	Size     uint
	Shared   bool
	Cert     Certificate
	ValidFor uint
	Private  PrivateKey
	Public   PublicKey
}

// Certificate is an x509.Certificate
type Certificate x509.Certificate

// MarshalJSON implements the json.Marshaler interface.
func (c Certificate) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.Raw)
}

// UnmarshalJSON implements the json.Marshaler interface.
func (c *Certificate) UnmarshalJSON(data []byte) error {
	// This function is called either when unmarshalling the manifest or the sealed
	// state. Thus, data can be a JSON object ({...}) or a JSON string ("...").

	if data[0] != '"' {
		// Unmarshal the JSON object to an x509.Certificate.
		return json.Unmarshal(data, (*x509.Certificate)(c))
	}

	// Unmarshal and parse the raw certificate.
	var raw []byte
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		return err
	}
	*c = Certificate(*cert)
	return nil
}

func encodeSecretDataToPem(data interface{}) (string, error) {
	var pemData []byte

	switch x := data.(type) {
	case Certificate:
		pemData = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: x.Raw})
	case PublicKey:
		pemData = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: x})
	case PrivateKey:
		pemData = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: x})
	default:
		return "", errors.New("invalid secret type")
	}

	return string(pemData), nil
}

func encodeSecretDataToHex(data interface{}) (string, error) {
	raw, err := encodeSecretDataToRaw(data)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString([]byte(raw)), nil
}

func encodeSecretDataToRaw(data interface{}) (string, error) {
	switch secret := data.(type) {
	case []byte:
		return string(secret), nil
	case PrivateKey:
		return string(secret), nil
	case PublicKey:
		return string(secret), nil
	case Secret:
		return string(secret.Public), nil
	case Certificate:
		return string(secret.Raw), nil
	default:
		return "", errors.New("invalid secret type")
	}
}

func encodeSecretDataToBase64(data interface{}) (string, error) {
	raw, err := encodeSecretDataToRaw(data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString([]byte(raw)), nil
}

var manifestTemplateFuncMap = template.FuncMap{
	"pem":    encodeSecretDataToPem,
	"hex":    encodeSecretDataToHex,
	"raw":    encodeSecretDataToRaw,
	"base64": encodeSecretDataToBase64,
}

// Check checks if the manifest is consistent.
func (m Manifest) Check(ctx context.Context) error {
	if len(m.Packages) <= 0 {
		return errors.New("no allowed packages defined")
	}
	if len(m.Marbles) <= 0 {
		return errors.New("no allowed marbles defined")
	}
	// if len(m.Infrastructures) <= 0 {
	// 	return errors.New("no allowed infrastructures defined")
	// }
	for _, marble := range m.Marbles {
		if _, ok := m.Packages[marble.Package]; !ok {
			return errors.New("manifest does not contain marble package " + marble.Package)
		}
	}
	return nil
}
