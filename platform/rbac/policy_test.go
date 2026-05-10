package rbac_test

import (
	"errors"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"
)

// fakeUser is a minimal PolicyUser used in policy tests.
type fakeUser struct {
	id          uuid.UUID
	admin       bool
	superAdmin  bool
	companyID   *uuid.UUID
}

func (u fakeUser) GetID() uuid.UUID            { return u.id }
func (u fakeUser) IsAdmin() bool               { return u.admin }
func (u fakeUser) IsSuperAdmin() bool          { return u.superAdmin }
func (u fakeUser) GetCompanyID() *uuid.UUID    { return u.companyID }

// adminAllowPolicy embeds BasePolicy and overrides Show/Update for admins.
type adminAllowPolicy struct {
	rbac.BasePolicy
}

func (p adminAllowPolicy) Show() bool   { return p.Admin }
func (p adminAllowPolicy) Update() bool { return p.Admin }

// customActionPolicy implements ActionPolicy.
type customActionPolicy struct {
	rbac.BasePolicy
}

func (p customActionPolicy) Can(action string) bool { return action == "publish" && p.Admin }

func TestBasePolicy_DefaultDeny(t *testing.T) {
	user := fakeUser{id: uuid.New(), admin: false}
	p := rbac.NewBasePolicy(user)
	if p.Show() || p.Create() || p.Update() || p.Destroy() {
		t.Errorf("BasePolicy default-deny violated: show=%v create=%v update=%v destroy=%v",
			p.Show(), p.Create(), p.Update(), p.Destroy())
	}
}

func TestAuthorize_StandardActions_Admin(t *testing.T) {
	user := fakeUser{id: uuid.New(), admin: true}
	p := adminAllowPolicy{BasePolicy: rbac.NewBasePolicy(user)}

	if err := rbac.Authorize(p, "show"); err != nil {
		t.Errorf("Authorize(show) admin err = %v, want nil", err)
	}
	if err := rbac.Authorize(p, "update"); err != nil {
		t.Errorf("Authorize(update) admin err = %v, want nil", err)
	}
	// Create + destroy still default-deny.
	if err := rbac.Authorize(p, "create"); !errors.Is(err, rbac.ErrUnauthorized) {
		t.Errorf("Authorize(create) admin err = %v, want ErrUnauthorized", err)
	}
}

func TestAuthorize_StandardActions_NonAdmin(t *testing.T) {
	user := fakeUser{id: uuid.New(), admin: false}
	p := adminAllowPolicy{BasePolicy: rbac.NewBasePolicy(user)}

	if err := rbac.Authorize(p, "show"); !errors.Is(err, rbac.ErrUnauthorized) {
		t.Errorf("Authorize(show) non-admin err = %v, want ErrUnauthorized", err)
	}
}

func TestAuthorize_CustomAction_Allowed(t *testing.T) {
	user := fakeUser{id: uuid.New(), admin: true}
	p := customActionPolicy{BasePolicy: rbac.NewBasePolicy(user)}

	if err := rbac.Authorize(p, "publish"); err != nil {
		t.Errorf("Authorize(publish) admin err = %v, want nil", err)
	}
}

func TestAuthorize_CustomAction_Denied(t *testing.T) {
	user := fakeUser{id: uuid.New(), admin: false}
	p := customActionPolicy{BasePolicy: rbac.NewBasePolicy(user)}

	if err := rbac.Authorize(p, "publish"); !errors.Is(err, rbac.ErrUnauthorized) {
		t.Errorf("Authorize(publish) non-admin err = %v, want ErrUnauthorized", err)
	}
}

func TestAuthorize_UnknownAction_NoActionPolicy(t *testing.T) {
	user := fakeUser{id: uuid.New(), admin: true}
	p := adminAllowPolicy{BasePolicy: rbac.NewBasePolicy(user)}

	err := rbac.Authorize(p, "publish")
	if !errors.Is(err, rbac.ErrUnknownAction) {
		t.Errorf("Authorize(publish) on plain Policy err = %v, want ErrUnknownAction", err)
	}
}

func TestAuthorize_UnknownAction_WithActionPolicy_FallsThrough(t *testing.T) {
	user := fakeUser{id: uuid.New(), admin: true}
	p := customActionPolicy{BasePolicy: rbac.NewBasePolicy(user)}

	// Action not handled by Can() returns ErrUnauthorized (not ErrUnknownAction).
	err := rbac.Authorize(p, "unknown_action")
	if !errors.Is(err, rbac.ErrUnauthorized) {
		t.Errorf("Authorize(unknown_action) ActionPolicy err = %v, want ErrUnauthorized", err)
	}
}

func TestNewBasePolicy_CachesAdmin(t *testing.T) {
	user := fakeUser{id: uuid.New(), admin: true, superAdmin: true}
	p := rbac.NewBasePolicy(user)
	if !p.Admin {
		t.Errorf("BasePolicy.Admin = false, want true")
	}
	if p.User.GetID() != user.id {
		t.Errorf("BasePolicy.User.GetID() = %v, want %v", p.User.GetID(), user.id)
	}
}
