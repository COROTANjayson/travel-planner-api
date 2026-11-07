package prep

import (
	"context"
	"errors"
	"strings"
	"testing"

	"travel-planner/travel-planner-api/internal/apperror"
)

type recordingStore struct {
	Store
	called      bool
	input       CreateInput
	update      UpdateInput
	anyAssignee bool
}

func (r *recordingStore) Create(_ context.Context, _, _ int64, in CreateInput) (Task, error) {
	r.called, r.input = true, in
	return Task{}, nil
}
func (r *recordingStore) Update(_ context.Context, _, _ int64, in UpdateInput) (Task, error) {
	r.called, r.update = true, in
	return Task{}, nil
}
func (r *recordingStore) List(context.Context, int64) ([]Task, error) {
	r.called = true
	return []Task{}, nil
}
func (r *recordingStore) Delete(context.Context, int64, int64) error { r.called = true; return nil }
func (r *recordingStore) Assign(context.Context, int64, int64, *int64) error {
	r.called = true
	return nil
}
func (r *recordingStore) Claim(context.Context, int64, int64, int64) error {
	r.called = true
	return nil
}
func (r *recordingStore) Drop(context.Context, int64, int64, int64) error {
	r.called = true
	return nil
}
func (r *recordingStore) Complete(context.Context, int64, int64, int64) error {
	r.called = true
	return nil
}
func (r *recordingStore) Reopen(_ context.Context, _, _, _ int64, anyAssignee bool) error {
	r.called, r.anyAssignee = true, anyAssignee
	return nil
}

// roleAuthorizer mirrors memberships.Service for one role; "" is a nonparticipant.
type roleAuthorizer string

func (a roleAuthorizer) check(allowed ...string) error {
	if a == "" {
		return apperror.ErrNotFound
	}
	for _, role := range allowed {
		if string(a) == role {
			return nil
		}
	}
	return apperror.ErrForbidden
}
func (a roleAuthorizer) RequireParticipant(context.Context, int64, int64) error {
	return a.check("owner", "editor", "member", "viewer")
}
func (a roleAuthorizer) RequireEditor(context.Context, int64, int64) error {
	return a.check("owner", "editor")
}
func (a roleAuthorizer) RequireNonViewer(context.Context, int64, int64) error {
	return a.check("owner", "editor", "member")
}

func TestRoleMatrix(t *testing.T) {
	ctx := context.Background()
	operations := map[string]func(*Service) error{
		"list": func(s *Service) error { _, err := s.List(ctx, 1, 2); return err },
		"create": func(s *Service) error {
			_, err := s.Create(ctx, 1, 2, CreateInput{Title: "Van", Kind: KindOnePerson})
			return err
		},
		"rename": func(s *Service) error {
			_, err := s.Update(ctx, 1, 2, 3, UpdateInput{Title: "Van"})
			return err
		},
		"delete":   func(s *Service) error { return s.Delete(ctx, 1, 2, 3) },
		"assign":   func(s *Service) error { return s.Assign(ctx, 1, 2, 3, AssigneeInput{}) },
		"claim":    func(s *Service) error { return s.Claim(ctx, 1, 2, 3) },
		"drop":     func(s *Service) error { return s.Drop(ctx, 1, 2, 3) },
		"complete": func(s *Service) error { return s.Complete(ctx, 1, 2, 3) },
		"reopen":   func(s *Service) error { return s.Reopen(ctx, 1, 2, 3) },
	}
	allowed := map[string][]string{
		"owner":  {"list", "create", "rename", "delete", "assign", "claim", "drop", "complete", "reopen"},
		"editor": {"list", "create", "rename", "delete", "assign", "claim", "drop", "complete", "reopen"},
		"member": {"list", "claim", "drop", "complete", "reopen"},
		"viewer": {"list"},
	}
	for role, names := range allowed {
		permitted := map[string]bool{}
		for _, name := range names {
			permitted[name] = true
		}
		for name, operation := range operations {
			store := &recordingStore{}
			err := operation(NewService(store, roleAuthorizer(role)))
			if permitted[name] {
				if err != nil || !store.called {
					t.Fatalf("%s %s: err %v, reached store %v", role, name, err, store.called)
				}
			} else if !errors.Is(err, apperror.ErrForbidden) || store.called {
				t.Fatalf("%s %s: got %v, reached store %v", role, name, err, store.called)
			}
		}
	}
	for name, operation := range operations {
		store := &recordingStore{}
		if err := operation(NewService(store, roleAuthorizer(""))); !errors.Is(err, apperror.ErrNotFound) || store.called {
			t.Fatalf("nonparticipant %s: got %v, reached store %v", name, err, store.called)
		}
	}
}

func TestReopenScope(t *testing.T) {
	for role, want := range map[string]bool{"owner": true, "editor": true, "member": false} {
		store := &recordingStore{}
		if err := NewService(store, roleAuthorizer(role)).Reopen(context.Background(), 1, 2, 3); err != nil || store.anyAssignee != want {
			t.Fatalf("%s: err %v, anyAssignee %v", role, err, store.anyAssignee)
		}
	}
}

func TestCreateValidation(t *testing.T) {
	assignee, invalid := int64(5), int64(0)
	long := strings.Repeat("a", 201)
	for name, in := range map[string]CreateInput{
		"blank title":       {Title: "   ", Kind: KindEveryone},
		"long title":        {Title: long, Kind: KindEveryone},
		"unknown kind":      {Title: "Van", Kind: "someone"},
		"missing kind":      {Title: "Van"},
		"everyone assignee": {Title: "Van", Kind: KindEveryone, AssigneeUserID: &assignee},
		"invalid assignee":  {Title: "Van", Kind: KindOnePerson, AssigneeUserID: &invalid},
	} {
		store := &recordingStore{}
		if _, err := NewService(store, roleAuthorizer("owner")).Create(context.Background(), 1, 2, in); !errors.Is(err, apperror.ErrInvalid) || store.called {
			t.Fatalf("%s: got %v, reached store %v", name, err, store.called)
		}
	}
	store := &recordingStore{}
	in := CreateInput{Title: "  Book the van  ", Kind: KindOnePerson, AssigneeUserID: &assignee}
	if _, err := NewService(store, roleAuthorizer("editor")).Create(context.Background(), 1, 2, in); err != nil || store.input.Title != "Book the van" {
		t.Fatalf("valid task: err %v, title %q", err, store.input.Title)
	}
}

func TestUpdateValidation(t *testing.T) {
	store := &recordingStore{}
	service := NewService(store, roleAuthorizer("owner"))
	if _, err := service.Update(context.Background(), 1, 2, 3, UpdateInput{Title: " "}); !errors.Is(err, apperror.ErrInvalid) || store.called {
		t.Fatalf("blank title: got %v, reached store %v", err, store.called)
	}
	if _, err := service.Update(context.Background(), 1, 2, 3, UpdateInput{Title: " Passport "}); err != nil || store.update.Title != "Passport" {
		t.Fatalf("valid title: err %v, title %q", err, store.update.Title)
	}
}

func TestActivityAndDateValidation(t *testing.T) {
	zero := int64(0)
	for _, date := range []string{"", "soon", "15/08/2026", "2026-02-30", "0000-01-01", "2026-8-1"} {
		store := &recordingStore{}
		service := NewService(store, roleAuthorizer("owner"))
		if _, err := service.Create(context.Background(), 1, 2, CreateInput{Title: "Van", Kind: KindEveryone, Date: &date}); !errors.Is(err, apperror.ErrInvalid) || store.called {
			t.Fatalf("create date %q: got %v, reached store %v", date, err, store.called)
		}
		if _, err := service.Update(context.Background(), 1, 2, 3, UpdateInput{Title: "Van", Date: &date}); !errors.Is(err, apperror.ErrInvalid) || store.called {
			t.Fatalf("update date %q: got %v, reached store %v", date, err, store.called)
		}
	}
	store := &recordingStore{}
	service := NewService(store, roleAuthorizer("owner"))
	if _, err := service.Create(context.Background(), 1, 2, CreateInput{Title: "Van", Kind: KindEveryone, ActivityID: &zero}); !errors.Is(err, apperror.ErrInvalid) || store.called {
		t.Fatalf("invalid activity: got %v, reached store %v", err, store.called)
	}
	activity, date := int64(9), "2026-08-15"
	if _, err := service.Update(context.Background(), 1, 2, 3, UpdateInput{Title: "Van", ActivityID: &activity, Date: &date}); err != nil || *store.update.ActivityID != 9 || *store.update.Date != date {
		t.Fatalf("valid details: err %v, update %+v", err, store.update)
	}
}

func TestAssignRejectsInvalidID(t *testing.T) {
	store, invalid := &recordingStore{}, int64(-1)
	if err := NewService(store, roleAuthorizer("owner")).Assign(context.Background(), 1, 2, 3, AssigneeInput{UserID: &invalid}); !errors.Is(err, apperror.ErrInvalid) || store.called {
		t.Fatalf("got %v, reached store %v", err, store.called)
	}
}
