//go:build windows

package authenticode

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cmsgSignerInfoParam = 6

var (
	errCertUntrustedRoot = syscall.Errno(0x800B0109)
	crypt32DLL           = windows.NewLazySystemDLL("crypt32.dll")
	cryptMsgGetParamProc = crypt32DLL.NewProc("CryptMsgGetParam")
	cryptMsgCloseProc    = crypt32DLL.NewProc("CryptMsgClose")
)

type cmsgSignerInfoHeader struct {
	Version      uint32
	Issuer       windows.CertNameBlob
	SerialNumber windows.CryptIntegerBlob
}

// VerifyFile 通过 Windows WinVerifyTrust 验证文件的 Authenticode 签名与系统信任链。
func VerifyFile(ctx context.Context, path string) error {
	return verifyWinTrust(ctx, path)
}

// VerifyMicrosoftFile verifies both the Windows trust chain and the signer identity.
// It is used before loading Microsoft-owned runtime code into an AgentDock process.
func VerifyMicrosoftFile(ctx context.Context, path string) error {
	if err := VerifyFile(ctx, path); err != nil {
		return fmt.Errorf("verify Microsoft Authenticode trust: %w", err)
	}
	certificate, err := signerCertificate(ctx, path)
	if err != nil {
		return fmt.Errorf("read Microsoft Authenticode signer: %w", err)
	}
	if !isMicrosoftCertificate(certificate) {
		return fmt.Errorf("Authenticode signer is not Microsoft Corporation: %s", certificate.Subject.String())
	}
	return nil
}

func isMicrosoftCertificate(certificate *x509.Certificate) bool {
	if certificate == nil {
		return false
	}
	for _, organization := range certificate.Subject.Organization {
		if strings.EqualFold(strings.TrimSpace(organization), "Microsoft Corporation") {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSpace(certificate.Subject.CommonName), "Microsoft Corporation")
}

// VerifyFileOrSameSigner first applies the normal Windows trust policy.
//
// AgentDock currently still supports releases signed by the existing self-signed
// project certificate. Windows reports those signatures as CERT_E_UNTRUSTEDROOT.
// In that one case only, accept the target when the already-installed reference
// binary has the same trust result and both files carry the exact same signer
// certificate thumbprint. This pins the update to the publisher identity already
// installed on the machine instead of trusting an arbitrary self-signed signer.
//
// When releases move to a publicly trusted Authenticode certificate, the normal
// WinVerifyTrust path succeeds and this compatibility path is bypassed.
func VerifyFileOrSameSigner(ctx context.Context, path, referencePath string) error {
	return verifyFileOrSameSignerWith(
		path,
		referencePath,
		func(candidate string) error { return verifyWinTrust(ctx, candidate) },
		func(candidate string) (string, error) { return signerThumbprint(ctx, candidate) },
	)
}

func verifyFileOrSameSignerWith(
	path string,
	referencePath string,
	verify func(string) error,
	thumbprint func(string) (string, error),
) error {
	verifyErr := verify(path)
	if verifyErr == nil {
		return nil
	}
	if !errors.Is(verifyErr, errCertUntrustedRoot) {
		return verifyErr
	}

	referenceErr := verify(referencePath)
	if !errors.Is(referenceErr, errCertUntrustedRoot) {
		return fmt.Errorf(
			"Authenticode target uses an untrusted self-signed chain but the installed publisher does not: %w",
			verifyErr,
		)
	}

	targetThumbprint, err := thumbprint(path)
	if err != nil {
		return fmt.Errorf("read Authenticode target signer: %w", err)
	}
	referenceThumbprint, err := thumbprint(referencePath)
	if err != nil {
		return fmt.Errorf("read installed Authenticode signer: %w", err)
	}
	if !strings.EqualFold(targetThumbprint, referenceThumbprint) {
		return fmt.Errorf(
			"Authenticode signer mismatch: target=%s installed=%s",
			targetThumbprint,
			referenceThumbprint,
		)
	}
	return nil
}

func verifyWinTrust(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode file path for Authenticode verification: %w", err)
	}
	fileInfo := &windows.WinTrustFileInfo{
		Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
		FilePath: pathUTF16,
	}
	trustData := &windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(fileInfo),
	}
	verifyErr := windows.WinVerifyTrustEx(
		windows.InvalidHWND,
		&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
		trustData,
	)
	trustData.StateAction = windows.WTD_STATEACTION_CLOSE
	closeErr := windows.WinVerifyTrustEx(
		windows.InvalidHWND,
		&windows.WINTRUST_ACTION_GENERIC_VERIFY_V2,
		trustData,
	)
	if verifyErr != nil {
		if closeErr != nil {
			return fmt.Errorf("Authenticode verification failed: %w (close trust state: %v)", verifyErr, closeErr)
		}
		return fmt.Errorf("Authenticode verification failed: %w", verifyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Authenticode trust state: %w", closeErr)
	}
	return nil
}

func signerThumbprint(ctx context.Context, path string) (string, error) {
	certificate, err := signerCertificate(ctx, path)
	if err != nil {
		return "", err
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	return strings.ToUpper(hex.EncodeToString(fingerprint[:])), nil
}

func signerCertificate(ctx context.Context, path string) (*x509.Certificate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("file path is empty")
	}
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode Authenticode file path: %w", err)
	}

	var (
		encodingType uint32
		contentType  uint32
		formatType   uint32
		certStore    windows.Handle
		message      windows.Handle
	)
	if err := windows.CryptQueryObject(
		windows.CERT_QUERY_OBJECT_FILE,
		unsafe.Pointer(pathUTF16),
		windows.CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED_EMBED,
		windows.CERT_QUERY_FORMAT_FLAG_BINARY,
		0,
		&encodingType,
		&contentType,
		&formatType,
		&certStore,
		&message,
		nil,
	); err != nil {
		return nil, fmt.Errorf("query Authenticode PKCS#7 signature: %w", err)
	}
	if certStore != 0 {
		defer windows.CertCloseStore(certStore, 0) //nolint:errcheck
	}
	if message != 0 {
		defer closeCryptMessage(message)
	}

	var signerInfoSize uint32
	if err := cryptMsgGetParam(message, cmsgSignerInfoParam, 0, nil, &signerInfoSize); err != nil {
		return nil, fmt.Errorf("read Authenticode signer size: %w", err)
	}
	if signerInfoSize < uint32(unsafe.Sizeof(cmsgSignerInfoHeader{})) {
		return nil, fmt.Errorf("Authenticode signer info is unexpectedly small: %d", signerInfoSize)
	}
	signerInfoBytes := make([]byte, signerInfoSize)
	if err := cryptMsgGetParam(
		message,
		cmsgSignerInfoParam,
		0,
		unsafe.Pointer(&signerInfoBytes[0]),
		&signerInfoSize,
	); err != nil {
		return nil, fmt.Errorf("read Authenticode signer info: %w", err)
	}
	signerInfo := (*cmsgSignerInfoHeader)(unsafe.Pointer(&signerInfoBytes[0]))
	certInfo := windows.CertInfo{
		Issuer:       signerInfo.Issuer,
		SerialNumber: signerInfo.SerialNumber,
	}
	certContext, err := windows.CertFindCertificateInStore(
		certStore,
		windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING,
		0,
		windows.CERT_FIND_SUBJECT_CERT,
		unsafe.Pointer(&certInfo),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("find Authenticode signer certificate: %w", err)
	}
	defer windows.CertFreeCertificateContext(certContext) //nolint:errcheck
	if certContext == nil || certContext.EncodedCert == nil || certContext.Length == 0 {
		return nil, errors.New("Authenticode signer certificate is empty")
	}
	certificateDER := append([]byte(nil), unsafe.Slice(certContext.EncodedCert, int(certContext.Length))...)
	certificate, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		return nil, fmt.Errorf("parse Authenticode signer certificate: %w", err)
	}
	return certificate, nil
}

func cryptMsgGetParam(
	message windows.Handle,
	paramType uint32,
	index uint32,
	data unsafe.Pointer,
	size *uint32,
) error {
	result, _, callErr := cryptMsgGetParamProc.Call(
		uintptr(message),
		uintptr(paramType),
		uintptr(index),
		uintptr(data),
		uintptr(unsafe.Pointer(size)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func closeCryptMessage(message windows.Handle) {
	if message == 0 {
		return
	}
	_, _, _ = cryptMsgCloseProc.Call(uintptr(message))
}
