package requestmeta

import "context"

type baseURLKey struct{}

func WithBaseURL(ctx context.Context, baseURL string) context.Context {
	if baseURL == "" {
		return ctx
	}
	return context.WithValue(ctx, baseURLKey{}, baseURL)
}

func BaseURL(ctx context.Context) string {
	value, _ := ctx.Value(baseURLKey{}).(string)
	return value
}
