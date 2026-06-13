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
	"time"

	"github.com/uvwt/agentdock/internal/artifactrelay"
)

func (c *Client) CreateDeviceArtifactFetch(ctx context.Context, credentials artifactrelay.DeviceCredentials, request artifactrelay.CreateFetchRequest) (artifactrelay.CreateFetchResult, error) {
	var response artifactrelay.CreateFetchResult
	endpoint := devicePath(credentials.DeviceID, "artifact-fetches")
	err := c.doJSON(ctx, c.httpClient, http.MethodPost, endpoint, credentials.DeviceToken, request, &response)
	return response, err
}

func (c *Client) GetDeviceArtifactFetch(ctx context.Context, credentials artifactrelay.DeviceCredentials, fetchID, fetchToken string) (artifactrelay.FetchJob, error) {
	var response artifactrelay.FetchJob
	endpoint := devicePath(credentials.DeviceID, "artifact-fetches", fetchID)
	err := c.doFetchJSON(ctx, http.MethodGet, endpoint, credentials.DeviceToken, "X-Artifact-Fetch-Token", fetchToken, nil, &response)
	return response, err
}

func (c *Client) ReportArtifactFetchResult(ctx context.Context, credentials artifactrelay.DeviceCredentials, resultPath, uploadToken string, request artifactrelay.FetchResultRequest) error {
	return c.doFetchJSON(ctx, http.MethodPost, resultPath, credentials.DeviceToken, "X-Artifact-Fetch-Upload-Token", uploadToken, request, nil)
}

func (c *Client) ConfirmArtifactFetchMounted(ctx context.Context, credentials artifactrelay.DeviceCredentials, fetchID, fetchToken string) (artifactrelay.FetchJob, error) {
	var response artifactrelay.FetchJob
	endpoint := devicePath(credentials.DeviceID, "artifact-fetches", fetchID, "mounted")
	err := c.doFetchJSON(ctx, http.MethodPost, endpoint, credentials.DeviceToken, "X-Artifact-Fetch-Token", fetchToken, map[string]any{}, &response)
	return response, err
}

func (c *Client) UploadArtifactFetch(ctx context.Context, credentials artifactrelay.DeviceCredentials, uploadPath, uploadToken, encryptedPath string, manifest artifactrelay.FetchManifest) (artifactrelay.FetchJob, error) {
	var response artifactrelay.FetchJob
	file, err := os.Open(encryptedPath)
	if err != nil {
		return response, fmt.Errorf("open encrypted fetch: %w", err)
	}
	defer file.Close()
	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	writeErr := make(chan error, 1)
	go func() {
		defer close(writeErr)
		defer pipeWriter.Close()
		part, err := writer.CreateFormField("manifest")
		if err != nil {
			writeErr <- err
			return
		}
		if err := json.NewEncoder(part).Encode(manifest); err != nil {
			writeErr <- err
			return
		}
		filePart, err := writer.CreateFormFile("file", filepath.Base(encryptedPath))
		if err != nil {
			writeErr <- err
			return
		}
		if _, err := io.Copy(filePart, file); err != nil {
			writeErr <- err
			return
		}
		if err := writer.Close(); err != nil {
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
	req.Header.Set("Authorization", "Bearer "+credentials.DeviceToken)
	req.Header.Set("X-Artifact-Fetch-Upload-Token", uploadToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		pipeReader.CloseWithError(err)
		<-writeErr
		return response, fmt.Errorf("upload encrypted fetch: %w", err)
	}
	defer resp.Body.Close()
	writerErr := <-writeErr
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if writerErr != nil {
		return response, fmt.Errorf("stream encrypted fetch: %w", writerErr)
	}
	if readErr != nil {
		return response, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return response, decodeAPIError(resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return response, err
	}
	return response, nil
}

func (c *Client) DownloadArtifactFetch(ctx context.Context, credentials artifactrelay.DeviceCredentials, fetchID, fetchToken string, output io.Writer) (artifactrelay.DownloadResult, error) {
	var result artifactrelay.DownloadResult
	endpoint := devicePath(credentials.DeviceID, "artifact-fetches", fetchID, "content")
	requestURL, err := c.resolveEndpoint(endpoint)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+credentials.DeviceToken)
	req.Header.Set("X-Artifact-Fetch-Token", fetchToken)
	req.Header.Set("Accept", "application/octet-stream")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("download encrypted fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return result, decodeAPIError(resp.StatusCode, body)
	}
	written, err := io.Copy(output, io.LimitReader(resp.Body, artifactrelay.MaxCipherBytes+1))
	if err != nil {
		return result, err
	}
	if written > artifactrelay.MaxCipherBytes {
		return result, fmt.Errorf("encrypted fetch exceeds %d bytes", artifactrelay.MaxCipherBytes)
	}
	result.Bytes = written
	result.CipherSHA256 = resp.Header.Get("X-Artifact-Cipher-SHA256")
	result.PlainSHA256 = resp.Header.Get("X-Artifact-Plain-SHA256")
	return result, nil
}

func (c *Client) doFetchJSON(ctx context.Context, method, endpoint, deviceToken, headerName, headerValue string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	requestURL, err := c.resolveEndpoint(endpoint)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	req.Header.Set(headerName, headerValue)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(resp.StatusCode, data)
	}
	if output != nil && len(data) > 0 {
		return json.Unmarshal(data, output)
	}
	return nil
}
