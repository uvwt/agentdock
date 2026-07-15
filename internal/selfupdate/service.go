package selfupdate

import "context"

type managedService interface {
	Name() string
	Restart(context.Context) error
}
