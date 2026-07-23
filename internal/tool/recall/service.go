package recall

import "github.com/uvwt/agentdock/internal/config"

type ConfigProvider func() config.Config

type Service struct {
	config ConfigProvider
}

func New(configProvider ConfigProvider) *Service {
	return &Service{config: configProvider}
}

const (
	MaxPrivateNoteSearchResults = maxPrivateNoteSearchResults
	MaxPrivateNoteReadBytes     = maxPrivateNoteReadBytes
)
