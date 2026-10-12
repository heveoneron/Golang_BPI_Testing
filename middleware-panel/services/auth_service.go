package services

import "middleware-panel/repository"

type AuthService interface {
	Authenticate(username, password string) (*repository.MiddlewareLoginResponse, error)
}

type authService struct {
	repo repository.AuthRepository
}

func NewAuthService(repo repository.AuthRepository) AuthService {
	return &authService{repo}
}

func (s *authService) Authenticate(username, password string) (*repository.MiddlewareLoginResponse, error) {
	return s.repo.CheckLoginViaAPI(username, password)
}
