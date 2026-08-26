package oauth

import "context"

type amazonQDriver struct{}

func (amazonQDriver) Start(ctx context.Context, s *Service, f flow) (flow, StartResult, error) {
	return startAWSBuilderID(ctx, s, f)
}
func (amazonQDriver) Complete(ctx context.Context, s *Service, f flow, _ string) (tokenResponse, error) {
	return completeAWSBuilderID(ctx, s, f)
}
