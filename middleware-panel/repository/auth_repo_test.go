package repository

import (
	"os"
	"testing"
)

func TestAuthRepo_LocalAdminFallback(t *testing.T) {
	// Pastikan LOGIN_API_URL kosong
	_ = os.Setenv("LOGIN_API_URL", "")
	_ = os.Setenv("PANEL_ADMIN_USER", "admin")
	_ = os.Setenv("PANEL_ADMIN_PASS", "admin")

	repo := NewAuthRepository()

	// Case 1: Login sukses dengan kredensial default admin/admin
	resp, err := repo.CheckLoginViaAPI("admin", "admin")
	if err != nil {
		t.Fatalf("expected successful login with admin/admin, got error: %v", err)
	}
	if resp.Status != "success" || resp.Token == "" {
		t.Fatalf("expected status 'success' and valid token, got: %+v", resp)
	}

	// Case 2: Login gagal jika password salah
	_, err = repo.CheckLoginViaAPI("admin", "wrong_pass")
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}

	// Case 3: Login gagal jika username salah
	_, err = repo.CheckLoginViaAPI("wrong_user", "admin")
	if err == nil {
		t.Fatal("expected error for wrong username, got nil")
	}
}
