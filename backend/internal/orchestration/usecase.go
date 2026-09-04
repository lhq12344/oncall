package orchestration

import "context"

type UseCase struct {
	Router RequestRouter
}

func NewUseCase(router RequestRouter) *UseCase {
	if router == nil {
		router = NewRouter(0.85)
	}
	return &UseCase{Router: router}
}

func (u *UseCase) Decide(ctx context.Context, input RouteInput) (RouteDecision, error) {
	return u.Router.Route(ctx, input)
}
