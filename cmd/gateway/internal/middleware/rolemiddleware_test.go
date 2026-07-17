package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRoleMiddlewareAllowsConfiguredRole(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT role").WithArgs("1001").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("operator"))

	called := false
	handler := NewRoleMiddleware(db, "operator", "reviewer").Handle(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if role := RoleFromContext(r.Context()); role != "operator" {
			t.Fatalf("role context = %q", role)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/activities", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxUserIDKey{}, "1001"))
	recorder := httptest.NewRecorder()

	handler(recorder, req)

	if !called || recorder.Code != http.StatusNoContent {
		t.Fatalf("called=%v status=%d", called, recorder.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRoleMiddlewareRejectsMissingOrForbiddenRole(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT role").WithArgs("1002").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("player"))

	handler := NewRoleMiddleware(db, "operator").Handle(func(http.ResponseWriter, *http.Request) {
		t.Fatal("forbidden request reached next handler")
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/activities", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxUserIDKey{}, "1002"))
	recorder := httptest.NewRecorder()

	handler(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRoleMiddlewareAdminBypassesSpecificRoleList(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT role").WithArgs("1003").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("admin"))

	handler := NewRoleMiddleware(db, "reviewer").Handle(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/activities/a/approve", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxUserIDKey{}, "1003"))
	recorder := httptest.NewRecorder()

	handler(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
}
