package controllers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// Identitas (by) harus selalu diambil dari sesi, termasuk bila browser mengirim Content-Type selain JSON.
func TestFinanceProxy_IdentityOverriddenForAnyContentType(t *testing.T) {
	var gotBody map[string]interface{}
	var gotCT string
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer engine.Close()
	t.Setenv("MIDDLEWARE_API_URL", engine.URL)

	r := gin.New()
	r.Use(sessions.Sessions("test_session", cookie.NewStore([]byte("secret_key_test"))))
	r.Use(func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("username", "checker01")
		c.Next()
	})
	fc := NewFinanceController()
	r.Any("/api/finance/*path", fc.Proxy)

	for _, tc := range []struct{ method, path, ct string }{
		{http.MethodPost, "/api/finance/assets/acquisitions/11111111-1111-1111-1111-111111111111/approve", "text/plain"},
		{http.MethodPut, "/api/finance/assets/acquisitions/11111111-1111-1111-1111-111111111111", ""},
	} {
		gotBody = nil
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"by":"maker01","amount":5}`))
		if tc.ct != "" {
			req.Header.Set("Content-Type", tc.ct)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: status %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if gotBody["by"] != "checker01" || gotBody["amount"] != float64(5) || gotCT != "application/json" {
			t.Errorf("%s %s: identity not enforced: body=%v ct=%q", tc.method, tc.path, gotBody, gotCT)
		}
	}
}
