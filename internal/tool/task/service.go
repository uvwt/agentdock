package task

import (
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/taskstate"
)

type ConfigProvider func() config.Config

type Service struct {
	config ConfigProvider
	tasks  *taskstate.Store
}

func New(configProvider ConfigProvider, tasks *taskstate.Store) *Service {
	return &Service{config: configProvider, tasks: tasks}
}
