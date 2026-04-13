package auth

import (
	"testing"
	"time"
)

func TestIssueAndParse(t *testing.T) {
	manager := NewManager("secret", time.Hour)

	token, _, err := manager.Issue(42, "admin", "super_admin")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if claims.AdminID != 42 {
		t.Fatalf("AdminID = %d, want 42", claims.AdminID)
	}
	if claims.Username != "admin" {
		t.Fatalf("Username = %q, want admin", claims.Username)
	}
	if claims.Role != "super_admin" {
		t.Fatalf("Role = %q, want super_admin", claims.Role)
	}
}
