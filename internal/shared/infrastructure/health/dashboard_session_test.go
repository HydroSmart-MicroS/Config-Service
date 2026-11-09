package health

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestStatusSessionIdleAndLogout(t *testing.T) {
	const origin = "https://status.example.test"
	const password = "panel-test-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	dashboard := &Dashboard{}
	if err := dashboard.RequireSession(origin, string(hash)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	dashboard.sessions.now = func() time.Time { return now }
	request := func(method, path, requestOrigin, form string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(form))
		r.Header.Set("Origin", requestOrigin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Authorization", "Basic bW9uaXRvcjppbnZhbGlk")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		dashboard.ServeHTTP(w, r)
		return w
	}
	want := func(w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("want HTTP %d, got %d", code, w.Code)
		}
	}
	form := url.Values{"username": {"monitor"}, "password": {password}}.Encode()
	login := func() *http.Cookie {
		w := request("POST", "/status/login", origin, form, nil)
		want(w, http.StatusSeeOther)
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/status/" {
			t.Fatal("insecure or missing status session cookie")
		}
		return cookies[0]
	}
	want(request("GET", "/status/services", "", "", nil), http.StatusUnauthorized)
	want(request("POST", "/status/login", "https://another.example", form, nil), http.StatusForbidden)
	cookie := login()
	now = now.Add(9 * time.Minute)
	passive := request("GET", "/status/services", "", "", cookie)
	want(passive, http.StatusOK)
	if passive.Header().Get("X-Status-Idle-Ms") != "60000" {
		t.Fatal("automatic refresh changed the idle deadline")
	}
	now = now.Add(time.Minute)
	want(request("GET", "/status/services", "", "", cookie), http.StatusUnauthorized)
	want(request("POST", "/status/activity", origin, "", cookie), http.StatusUnauthorized)
	cookie = login()
	now = now.Add(9 * time.Minute)
	want(request("POST", "/status/activity", "https://another.example", "", cookie), http.StatusForbidden)
	want(request("POST", "/status/activity", origin, "", cookie), http.StatusNoContent)
	now = now.Add(2 * time.Minute)
	want(request("GET", "/status/services", "", "", cookie), http.StatusOK)
	out := request("POST", "/status/logout", origin, "", cookie)
	want(out, http.StatusSeeOther)
	if cookies := out.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatal("logout did not clear the cookie")
	}
	want(request("GET", "/status/services", "", "", cookie), http.StatusUnauthorized)
}
