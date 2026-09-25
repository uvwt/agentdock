package oauthclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type discoveredAuth struct {
	Resource        string
	Issuer          string
	Metadata        *oauthex.AuthServerMeta
	RequestedScopes []string
}

type metadataCandidate struct {
	metadataURL string
	resourceURL string
}

func discoverAuthorization(ctx context.Context, client *http.Client, endpoint string, challengeHeaders []string) (discoveredAuth, error) {
	challenges, err := oauthex.ParseWWWAuthenticate(challengeHeaders)
	if err != nil {
		return discoveredAuth{}, fmt.Errorf("parse OAuth challenge: %w", err)
	}
	resourceMetadataURL := ""
	requestedScopes := []string(nil)
	for _, challenge := range challenges {
		if challenge.Scheme != "bearer" {
			continue
		}
		if resourceMetadataURL == "" {
			resourceMetadataURL = strings.TrimSpace(challenge.Params["resource_metadata"])
		}
		if len(requestedScopes) == 0 {
			requestedScopes = strings.Fields(challenge.Params["scope"])
		}
	}

	var resource *oauthex.ProtectedResourceMetadata
	candidates := protectedResourceMetadataCandidates(endpoint, resourceMetadataURL)
	for index, candidate := range candidates {
		metadata, metadataErr := oauthex.GetProtectedResourceMetadata(ctx, candidate.metadataURL, candidate.resourceURL, client)
		if metadataErr != nil {
			// 服务端在 challenge 中显式指定 resource_metadata 时，它就是本次
			// 授权边界；不能请求失败后静默降级到猜测地址，否则可能把服务端
			// 明确的 resource/issuer 约束绕过去。
			if resourceMetadataURL != "" && index == 0 {
				return discoveredAuth{}, fmt.Errorf("load challenged protected resource metadata: %w", metadataErr)
			}
			continue
		}
		resource = metadata
		break
	}
	if resource == nil {
		// 兼容 MCP 2025-03-26：没有 Protected Resource Metadata 时，MCP endpoint
		// 所在 origin 同时作为 Authorization Server，resource 仍绑定完整 MCP URL。
		u, err := url.Parse(endpoint)
		if err != nil {
			return discoveredAuth{}, fmt.Errorf("parse MCP endpoint: %w", err)
		}
		u.Path, u.RawPath, u.RawQuery, u.Fragment = "", "", "", ""
		resource = &oauthex.ProtectedResourceMetadata{
			Resource:             endpoint,
			AuthorizationServers: []string{strings.TrimRight(u.String(), "/")},
		}
	}
	if len(resource.AuthorizationServers) == 0 {
		return discoveredAuth{}, errors.New("protected resource metadata omitted authorization_servers")
	}
	issuer := strings.TrimSpace(resource.AuthorizationServers[0])
	metadata, err := sdkauth.GetAuthServerMetadata(ctx, issuer, client)
	if err != nil {
		return discoveredAuth{}, fmt.Errorf("discover authorization server metadata: %w", err)
	}
	if metadata == nil {
		base := strings.TrimRight(issuer, "/")
		metadata = &oauthex.AuthServerMeta{
			Issuer:                issuer,
			AuthorizationEndpoint: base + "/authorize",
			TokenEndpoint:         base + "/token",
			RegistrationEndpoint:  base + "/register",
		}
	}
	if strings.TrimSpace(metadata.AuthorizationEndpoint) == "" || strings.TrimSpace(metadata.TokenEndpoint) == "" {
		return discoveredAuth{}, errors.New("authorization server metadata omitted required endpoints")
	}
	if len(requestedScopes) == 0 {
		requestedScopes = append([]string(nil), resource.ScopesSupported...)
	}
	if slices.Contains(metadata.ScopesSupported, "offline_access") && !slices.Contains(requestedScopes, "offline_access") {
		requestedScopes = append(requestedScopes, "offline_access")
	}
	return discoveredAuth{
		Resource:        resource.Resource,
		Issuer:          metadata.Issuer,
		Metadata:        metadata,
		RequestedScopes: uniqueStrings(requestedScopes),
	}, nil
}

func protectedResourceMetadataCandidates(endpoint, challengeURL string) []metadataCandidate {
	result := make([]metadataCandidate, 0, 3)
	if strings.TrimSpace(challengeURL) != "" {
		result = append(result, metadataCandidate{metadataURL: strings.TrimSpace(challengeURL), resourceURL: endpoint})
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return result
	}
	u.RawQuery, u.Fragment = "", ""
	pathResource := endpoint
	pathMetadata := *u
	pathMetadata.Path = "/.well-known/oauth-protected-resource/" + strings.TrimLeft(u.Path, "/")
	pathMetadata.RawPath = ""
	result = append(result, metadataCandidate{metadataURL: pathMetadata.String(), resourceURL: pathResource})

	rootResource := *u
	rootResource.Path, rootResource.RawPath = "", ""
	rootMetadata := rootResource
	rootMetadata.Path = "/.well-known/oauth-protected-resource"
	result = append(result, metadataCandidate{metadataURL: rootMetadata.String(), resourceURL: rootResource.String()})
	return result
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
