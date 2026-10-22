package services

import (
	"errors"
	"middleware-panel/models"
	"middleware-panel/repository"
	"strings"
)

type UserService interface {
	GetUsers() ([]models.User, error)
	GetUserByID(id uint) (*models.User, error)
	CreateUser(u *models.User) error
	UpdateUser(u *models.User, updatePassword bool) error
	ToggleActive(id uint) error
	DeleteUser(id uint, currentUsername string) error
	SeedAdminUser() error
}

type userService struct {
	repo repository.UserRepository
}

func NewUserService(repo repository.UserRepository) UserService {
	return &userService{repo: repo}
}

func (s *userService) GetUsers() ([]models.User, error) {
	return s.repo.GetAll()
}

func (s *userService) GetUserByID(id uint) (*models.User, error) {
	return s.repo.GetByID(id)
}

func (s *userService) CreateUser(u *models.User) error {
	u.Username = strings.TrimSpace(u.Username)
	if u.Username == "" {
		return errors.New("Username tidak boleh kosong")
	}
	if u.Password == "" {
		return errors.New("Password tidak boleh kosong")
	}
	if u.Role == "" {
		u.Role = "operator"
	}
	return s.repo.Create(u)
}

func (s *userService) UpdateUser(u *models.User, updatePassword bool) error {
	return s.repo.Update(u, updatePassword)
}

func (s *userService) ToggleActive(id uint) error {
	return s.repo.ToggleActive(id)
}

func (s *userService) DeleteUser(id uint, currentUsername string) error {
	user, err := s.repo.GetByID(id)
	if err != nil {
		return errors.New("Pengguna tidak ditemukan")
	}
	if strings.EqualFold(user.Username, currentUsername) {
		return errors.New("Tidak dapat menghapus akun yang sedang Anda gunakan untuk login saat ini")
	}
	return s.repo.Delete(id)
}

func (s *userService) SeedAdminUser() error {
	return s.repo.SeedAdminUser()
}
