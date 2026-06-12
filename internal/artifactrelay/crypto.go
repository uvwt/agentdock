package artifactrelay

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

var containerMagic = [4]byte{'A', 'D', 'R', '1'}

type EncryptResult struct {
	FileKey             []byte
	EphemeralPrivateKey []byte
	EphemeralPublicKey  string
	PlainSize           int64
	PlainSHA256         string
	CipherSize          int64
	CipherSHA256        string
}

type DecryptResult struct {
	PlainSize   int64
	PlainSHA256 string
}

func EncryptFile(inputPath, outputPath string) (EncryptResult, error) {
	input, err := os.Open(inputPath)
	if err != nil {
		return EncryptResult{}, fmt.Errorf("open artifact source: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return EncryptResult{}, fmt.Errorf("create encrypted artifact: %w", err)
	}
	failed := true
	defer func() {
		_ = output.Close()
		if failed {
			_ = os.Remove(outputPath)
		}
	}()

	fileKey := make([]byte, 32)
	if _, err := rand.Read(fileKey); err != nil {
		return EncryptResult{}, fmt.Errorf("generate artifact file key: %w", err)
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return EncryptResult{}, fmt.Errorf("generate ephemeral X25519 key: %w", err)
	}
	block, err := aes.NewCipher(fileKey)
	if err != nil {
		return EncryptResult{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptResult{}, err
	}
	noncePrefix := make([]byte, 8)
	if _, err := rand.Read(noncePrefix); err != nil {
		return EncryptResult{}, fmt.Errorf("generate artifact nonce prefix: %w", err)
	}

	cipherHash := sha256.New()
	writer := io.MultiWriter(output, cipherHash)
	header := make([]byte, 16)
	copy(header[:4], containerMagic[:])
	binary.BigEndian.PutUint32(header[4:8], uint32(DefaultChunkSize))
	copy(header[8:16], noncePrefix)
	cipherSize, err := writeAllCount(writer, header)
	if err != nil {
		return EncryptResult{}, fmt.Errorf("write artifact header: %w", err)
	}

	plainHash := sha256.New()
	reader := bufio.NewReaderSize(io.TeeReader(input, plainHash), DefaultChunkSize)
	buffer := make([]byte, DefaultChunkSize)
	var plainSize int64
	var chunkIndex uint32
	for {
		n, readErr := io.ReadFull(reader, buffer)
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return EncryptResult{}, fmt.Errorf("read artifact source: %w", readErr)
		}
		if n > 0 {
			nonce := chunkNonce(noncePrefix, chunkIndex)
			ciphertext := gcm.Seal(nil, nonce, buffer[:n], chunkAAD(chunkIndex))
			frameHeader := make([]byte, 4)
			binary.BigEndian.PutUint32(frameHeader, uint32(len(ciphertext)))
			written, err := writeAllCount(writer, frameHeader)
			cipherSize += written
			if err != nil {
				return EncryptResult{}, fmt.Errorf("write artifact frame header: %w", err)
			}
			written, err = writeAllCount(writer, ciphertext)
			cipherSize += written
			if err != nil {
				return EncryptResult{}, fmt.Errorf("write artifact frame: %w", err)
			}
			plainSize += int64(n)
			if chunkIndex == ^uint32(0) {
				return EncryptResult{}, errors.New("artifact contains too many encryption chunks")
			}
			chunkIndex++
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
	}
	terminator := []byte{0, 0, 0, 0}
	written, err := writeAllCount(writer, terminator)
	cipherSize += written
	if err != nil {
		return EncryptResult{}, fmt.Errorf("write artifact terminator: %w", err)
	}
	if err := output.Sync(); err != nil {
		return EncryptResult{}, fmt.Errorf("sync encrypted artifact: %w", err)
	}
	if err := output.Close(); err != nil {
		return EncryptResult{}, fmt.Errorf("close encrypted artifact: %w", err)
	}
	failed = false
	return EncryptResult{
		FileKey: fileKey, EphemeralPrivateKey: ephemeral.Bytes(),
		EphemeralPublicKey: base64.RawURLEncoding.EncodeToString(ephemeral.PublicKey().Bytes()),
		PlainSize:          plainSize, PlainSHA256: hex.EncodeToString(plainHash.Sum(nil)),
		CipherSize: cipherSize, CipherSHA256: hex.EncodeToString(cipherHash.Sum(nil)),
	}, nil
}

func DecryptFile(inputPath, outputPath string, fileKey []byte) (DecryptResult, error) {
	if len(fileKey) != 32 {
		return DecryptResult{}, errors.New("artifact file key must be 32 bytes")
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return DecryptResult{}, fmt.Errorf("open encrypted artifact: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return DecryptResult{}, fmt.Errorf("create decrypted artifact: %w", err)
	}
	failed := true
	defer func() {
		_ = output.Close()
		if failed {
			_ = os.Remove(outputPath)
		}
	}()
	block, err := aes.NewCipher(fileKey)
	if err != nil {
		return DecryptResult{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return DecryptResult{}, err
	}
	header := make([]byte, 16)
	if _, err := io.ReadFull(input, header); err != nil {
		return DecryptResult{}, errors.New("artifact container header is truncated")
	}
	if !equalBytes(header[:4], containerMagic[:]) {
		return DecryptResult{}, errors.New("artifact container magic is invalid")
	}
	chunkSize := binary.BigEndian.Uint32(header[4:8])
	if chunkSize == 0 || chunkSize > 16<<20 {
		return DecryptResult{}, errors.New("artifact chunk size is invalid")
	}
	noncePrefix := header[8:16]
	plainHash := sha256.New()
	writer := io.MultiWriter(output, plainHash)
	var plainSize int64
	var chunkIndex uint32
	for {
		frameHeader := make([]byte, 4)
		if _, err := io.ReadFull(input, frameHeader); err != nil {
			return DecryptResult{}, errors.New("artifact frame header is truncated")
		}
		frameSize := binary.BigEndian.Uint32(frameHeader)
		if frameSize == 0 {
			break
		}
		if frameSize < uint32(gcm.Overhead()) || frameSize > chunkSize+uint32(gcm.Overhead()) {
			return DecryptResult{}, errors.New("artifact frame size is invalid")
		}
		ciphertext := make([]byte, frameSize)
		if _, err := io.ReadFull(input, ciphertext); err != nil {
			return DecryptResult{}, errors.New("artifact frame is truncated")
		}
		plaintext, err := gcm.Open(nil, chunkNonce(noncePrefix, chunkIndex), ciphertext, chunkAAD(chunkIndex))
		if err != nil {
			return DecryptResult{}, errors.New("artifact authentication failed")
		}
		if _, err := writer.Write(plaintext); err != nil {
			return DecryptResult{}, fmt.Errorf("write decrypted artifact: %w", err)
		}
		plainSize += int64(len(plaintext))
		if chunkIndex == ^uint32(0) {
			return DecryptResult{}, errors.New("artifact contains too many decryption chunks")
		}
		chunkIndex++
	}
	trailing := make([]byte, 1)
	if n, err := input.Read(trailing); err != io.EOF || n != 0 {
		return DecryptResult{}, errors.New("artifact container has trailing data")
	}
	if err := output.Sync(); err != nil {
		return DecryptResult{}, fmt.Errorf("sync decrypted artifact: %w", err)
	}
	if err := output.Close(); err != nil {
		return DecryptResult{}, fmt.Errorf("close decrypted artifact: %w", err)
	}
	failed = false
	return DecryptResult{PlainSize: plainSize, PlainSHA256: hex.EncodeToString(plainHash.Sum(nil))}, nil
}

func WrapFileKey(ephemeralPrivate []byte, targetPublicEncoded string, fileKey []byte, artifactID, deliveryID, targetDeviceID string) (wrappedKey, nonce string, err error) {
	if len(fileKey) != 32 {
		return "", "", errors.New("artifact file key must be 32 bytes")
	}
	curve := ecdh.X25519()
	private, err := curve.NewPrivateKey(ephemeralPrivate)
	if err != nil {
		return "", "", errors.New("ephemeral X25519 private key is invalid")
	}
	targetBytes, err := base64.RawURLEncoding.DecodeString(targetPublicEncoded)
	if err != nil || len(targetBytes) != 32 {
		return "", "", errors.New("target X25519 public key is invalid")
	}
	target, err := curve.NewPublicKey(targetBytes)
	if err != nil {
		return "", "", errors.New("target X25519 public key is invalid")
	}
	shared, err := private.ECDH(target)
	if err != nil {
		return "", "", errors.New("derive artifact wrapping secret failed")
	}
	aad := deliveryAAD(artifactID, deliveryID, targetDeviceID)
	wrappingKey := hkdfSHA256(shared, []byte("AgentDock Artifact Relay ADR1"), aad, 32)
	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonceBytes := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", "", err
	}
	wrapped := gcm.Seal(nil, nonceBytes, fileKey, aad)
	return base64.RawURLEncoding.EncodeToString(wrapped), base64.RawURLEncoding.EncodeToString(nonceBytes), nil
}

func UnwrapFileKey(localPrivate []byte, ephemeralPublicEncoded, wrappedEncoded, nonceEncoded, artifactID, deliveryID, targetDeviceID string) ([]byte, error) {
	curve := ecdh.X25519()
	private, err := curve.NewPrivateKey(localPrivate)
	if err != nil {
		return nil, errors.New("local artifact X25519 private key is invalid")
	}
	ephemeralBytes, err := base64.RawURLEncoding.DecodeString(ephemeralPublicEncoded)
	if err != nil || len(ephemeralBytes) != 32 {
		return nil, errors.New("ephemeral artifact X25519 public key is invalid")
	}
	ephemeral, err := curve.NewPublicKey(ephemeralBytes)
	if err != nil {
		return nil, errors.New("ephemeral artifact X25519 public key is invalid")
	}
	shared, err := private.ECDH(ephemeral)
	if err != nil {
		return nil, errors.New("derive artifact unwrapping secret failed")
	}
	aad := deliveryAAD(artifactID, deliveryID, targetDeviceID)
	wrappingKey := hkdfSHA256(shared, []byte("AgentDock Artifact Relay ADR1"), aad, 32)
	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	wrapped, wrappedErr := base64.RawURLEncoding.DecodeString(wrappedEncoded)
	nonce, nonceErr := base64.RawURLEncoding.DecodeString(nonceEncoded)
	if wrappedErr != nil || nonceErr != nil || len(nonce) != gcm.NonceSize() {
		return nil, errors.New("wrapped artifact key metadata is invalid")
	}
	fileKey, err := gcm.Open(nil, nonce, wrapped, aad)
	if err != nil || len(fileKey) != 32 {
		return nil, errors.New("wrapped artifact key authentication failed")
	}
	return fileKey, nil
}

func chunkNonce(prefix []byte, index uint32) []byte {
	nonce := make([]byte, 12)
	copy(nonce[:8], prefix)
	binary.BigEndian.PutUint32(nonce[8:], index)
	return nonce
}

func chunkAAD(index uint32) []byte {
	result := make([]byte, 8)
	copy(result[:4], containerMagic[:])
	binary.BigEndian.PutUint32(result[4:], index)
	return result
}

func deliveryAAD(artifactID, deliveryID, targetDeviceID string) []byte {
	return []byte(FormatVersion + "\x00" + artifactID + "\x00" + deliveryID + "\x00" + targetDeviceID)
}

func hkdfSHA256(secret, salt, info []byte, length int) []byte {
	extractor := hmac.New(sha256.New, salt)
	_, _ = extractor.Write(secret)
	prk := extractor.Sum(nil)
	result := make([]byte, 0, length)
	var previous []byte
	for counter := byte(1); len(result) < length; counter++ {
		expander := hmac.New(sha256.New, prk)
		_, _ = expander.Write(previous)
		_, _ = expander.Write(info)
		_, _ = expander.Write([]byte{counter})
		previous = expander.Sum(nil)
		remaining := length - len(result)
		if remaining < len(previous) {
			result = append(result, previous[:remaining]...)
		} else {
			result = append(result, previous...)
		}
	}
	return result
}

func writeAllCount(writer io.Writer, data []byte) (int64, error) {
	var total int64
	for len(data) > 0 {
		n, err := writer.Write(data)
		total += int64(n)
		data = data[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
