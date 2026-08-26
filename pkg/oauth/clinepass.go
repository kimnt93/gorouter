package oauth

import "context"

type clinePassDriver struct{}

func (clinePassDriver) Start(_ context.Context, _ *Service, f flow) (flow, StartResult, error) {
	return startCline(f)
}
func (clinePassDriver) Complete(ctx context.Context, s *Service, f flow, callback string) (tokenResponse, error) {
	return completeCline(ctx, s, f, callback)
}
