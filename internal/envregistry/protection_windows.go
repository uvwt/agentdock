//go:build windows

package envregistry

import (
	"encoding/base64"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsDPAPIProtection = "windows-dpapi-user-v1"

var dpapiEntropy = []byte("agentdock.envregistry.v1")

func protectValuesForStorage(values valuesFile) (valuesFile, error) {
	protected := cloneValues(values)
	protected.Protection = windowsDPAPIProtection
	for name, value := range protected.Secrets {
		ciphertext, err := dpapiProtect([]byte(value))
		if err != nil {
			return valuesFile{}, fmt.Errorf("protect secret %s with Windows DPAPI: %w", name, err)
		}
		protected.Secrets[name] = base64.StdEncoding.EncodeToString(ciphertext)
	}
	return protected, nil
}

func unprotectValuesFromStorage(values valuesFile) (valuesFile, error) {
	// Files without a protection marker are legacy plaintext and are accepted only
	// as migration input. The next Set/Delete rewrites them using current-user DPAPI.
	if values.Protection == "" {
		return cloneValues(values), nil
	}
	if values.Protection != windowsDPAPIProtection {
		return valuesFile{}, errUnsupportedProtection(values.Protection)
	}
	plain := cloneValues(values)
	plain.Protection = ""
	for name, encoded := range values.Secrets {
		ciphertext, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return valuesFile{}, fmt.Errorf("decode protected secret %s: %w", name, err)
		}
		value, err := dpapiUnprotect(ciphertext)
		if err != nil {
			return valuesFile{}, fmt.Errorf("unprotect secret %s with Windows DPAPI: %w", name, err)
		}
		plain.Secrets[name] = string(value)
	}
	return plain, nil
}

func dpapiProtect(value []byte) ([]byte, error) {
	input := dataBlob(value)
	entropy := dataBlob(dpapiEntropy)
	var output windows.DataBlob
	if err := windows.CryptProtectData(
		&input,
		nil,
		&entropy,
		0,
		nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN,
		&output,
	); err != nil {
		return nil, err
	}
	return copyAndFreeBlob(output), nil
}

func dpapiUnprotect(value []byte) ([]byte, error) {
	input := dataBlob(value)
	entropy := dataBlob(dpapiEntropy)
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(
		&input,
		nil,
		&entropy,
		0,
		nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN,
		&output,
	); err != nil {
		return nil, err
	}
	return copyAndFreeBlob(output), nil
}

func dataBlob(value []byte) windows.DataBlob {
	blob := windows.DataBlob{Size: uint32(len(value))}
	if len(value) > 0 {
		blob.Data = &value[0]
	}
	return blob
}

func copyAndFreeBlob(blob windows.DataBlob) []byte {
	if blob.Data == nil {
		return nil
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(blob.Data)))
	return append([]byte(nil), unsafe.Slice(blob.Data, blob.Size)...)
}
