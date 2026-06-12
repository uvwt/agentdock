package nexusclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/artifactrelay"
)

func (c *Client) CreateDeviceArtifactUpload(ctx context.Context, credentials artifactrelay.DeviceCredentials, request artifactrelay.CreateUploadRequest) (artifactrelay.CreateUploadResult, error) {
	var response artifactrelay.CreateUploadResult
	endpoint := devicePath(credentials.DeviceID, "artifacts", "uploads")
	err := c.doJSON(ctx, c.httpClient, http.MethodPost, endpoint, credentials.DeviceToken, request, &response)
	return response, err
}

func (c *Client) UploadArtifact(ctx context.Context, uploadPath, uploadToken, encryptedPath string, manifest artifactrelay.UploadManifest) (artifactrelay.UploadCompletion, error) {
	var response artifactrelay.UploadCompletion
	file, err := os.Open(encryptedPath)
	if err != nil {
		return response, fmt.Errorf("open encrypted artifact: %w", err)
	}
	defer file.Close()
	pipeReader, pipeWriter := io.Pipe()
	multipartWriter := multipart.NewWriter(pipeWriter)
	writeErr := make(chan error, 1)
	go func() {
		defer close(writeErr)
		defer pipeWriter.Close()
		manifestPart, err := multipartWriter.CreateFormField("manifest")
		if err != nil {
			writeErr <- err
			return
		}
		if err := json.NewEncoder(manifestPart).Encode(manifest); err != nil {
			writeErr <- err
			return
		}
		filePart, err := multipartWriter.CreateFormFile("file", filepath.Base(encryptedPath))
		if err != nil {
			writeErr <- err
			return
		}
		if _, err := io.Copy(filePart, file); err != nil {
			writeErr <- err
			return
		}
		if err := multipartWriter.Close(); err != nil {
			writeErr <- err
			return
		}
		writeErr <- nil
	}()

	requestURL, err := c.resolveEndpoint(uploadPath)
	if err != nil {
		pipeReader.CloseWithError(err)
		<-writeErr
		return response, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, pipeReader)
	if err != nil {
		pipeReader.CloseWithError(err)
		<-writeErr
		return response, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req.Header.Set("X-Artifact-Upload-Token", uploadToken)
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		pipeReader.CloseWithError(err)
		<-writeErr
		return response, fmt.Errorf("upload encrypted artifact: %w", err)
	}
	defer resp.Body.Close()
	writerErr := <-writeErr
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if writerErr != nil {
		return response, fmt.Errorf("stream encrypted artifact: %w", writerErr)
	}
	if readErr != nil {
		return response, fmt.Errorf("read artifact upload response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return response, decodeAPIError(resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return response, fmt.Errorf("decode artifact upload response: %w", err)
	}
	return response, nil
}

func (c *Client) DownloadArtifact(ctx context.Context, credentials artifactrelay.DeviceCredentials, downloadPath, deliveryToken string, output io.Writer) (artifactrelay.DownloadResult, error) {
	var result artifactrelay.DownloadResult
	requestURL, err := c.resolveEndpoint(downloadPath)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+credentials.DeviceToken)
	req.Header.Set("X-Artifact-Delivery-Token", deliveryToken)
	req.Header.Set("Accept", "application/octet-stream")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("download encrypted artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return result, decodeAPIError(resp.StatusCode, body)
	}
	written, err := io.Copy(output, io.LimitReader(resp.Body, artifactrelay.MaxCipherBytes+1))
	if err != nil {
		return result, fmt.Errorf("store encrypted artifact: %w", err)
	}
	if written > artifactrelay.MaxCipherBytes {
		return result, fmt.Errorf("encrypted artifact exceeds %d bytes", artifactrelay.MaxCipherBytes)
	}
	result.Bytes = written
	result.CipherSHA256 = strings.TrimSpace(resp.Header.Get("X-Artifact-Cipher-SHA256"))
	result.PlainSHA256 = strings.TrimSpace(resp.Header.Get("X-Artifact-Plain-SHA256"))
	return result, nil
}

func (c *Client) ReportArtifactResult(ctx context.Context, credentials artifactrelay.DeviceCredentials, resultPath, deliveryToken string, request artifactrelay.DeliveryResultRequest) error {
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	requestURL, err := c.resolveEndpoint(resultPath)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+credentials.DeviceToken)
	req.Header.Set("X-Artifact-Delivery-Token", deliveryToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("report artifact delivery: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(resp.StatusCode, responseBody)
	}
	return nil
}

func (c *Client) resolveEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || !strings.HasPrefix(endpoint, "/v1/") || strings.Contains(endpoint, "..") {
		return "", fmt.Errorf("invalid Nexus artifact endpoint")
	}
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimSuffix(c.baseURL.Path, "/") + endpoint
	requestURL.RawQuery = ""
	requestURL.Fragment = ""
	return requestURL.String(), nil
}
