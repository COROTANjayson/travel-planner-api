package prep

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"travel-planner/travel-planner-api/internal/apperror"
)

type Store interface {
	List(context.Context, int64) ([]Task, error)
	Create(context.Context, int64, int64, CreateInput) (Task, error)
	Update(context.Context, int64, int64, UpdateInput) (Task, error)
	Delete(context.Context, int64, int64) error
	Assign(context.Context, int64, int64, *int64) error
	Claim(context.Context, int64, int64, int64) error
	Drop(context.Context, int64, int64, int64) error
	Complete(context.Context, int64, int64, int64) error
	Reopen(context.Context, int64, int64, int64, bool) error
}

type Authorizer interface {
	RequireParticipant(context.Context, int64, int64) error
	RequireEditor(context.Context, int64, int64) error
	RequireNonViewer(context.Context, int64, int64) error
}

type Service struct {
	store Store
	authz Authorizer
}

func NewService(store Store, authz Authorizer) *Service { return &Service{store: store, authz: authz} }

func cleanTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 200 {
		return "", fmt.Errorf("%w: title must contain 1 to 200 bytes", apperror.ErrInvalid)
	}
	return title, nil
}

func validAssignee(id *int64) error {
	if id != nil && *id <= 0 {
		return fmt.Errorf("%w: assignee must be a positive user ID", apperror.ErrInvalid)
	}
	return nil
}

func validDetails(activityID *int64, date *string) error {
	if activityID != nil && *activityID <= 0 {
		return fmt.Errorf("%w: activity must be a positive ID", apperror.ErrInvalid)
	}
	if date != nil {
		parsed, err := time.Parse(time.DateOnly, *date)
		if err != nil || parsed.Year() < 1 {
			return fmt.Errorf("%w: date must be a valid YYYY-MM-DD date", apperror.ErrInvalid)
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context, userID, tripID int64) ([]Task, error) {
	if err := s.authz.RequireParticipant(ctx, userID, tripID); err != nil {
		return nil, err
	}
	return s.store.List(ctx, tripID)
}

func (s *Service) Create(ctx context.Context, userID, tripID int64, in CreateInput) (Task, error) {
	if err := s.authz.RequireEditor(ctx, userID, tripID); err != nil {
		return Task{}, err
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return Task{}, err
	}
	in.Title = title
	if in.Kind != KindOnePerson && in.Kind != KindEveryone {
		return Task{}, fmt.Errorf("%w: kind must be one_person or everyone", apperror.ErrInvalid)
	}
	if in.Kind == KindEveryone && in.AssigneeUserID != nil {
		return Task{}, fmt.Errorf("%w: everyone tasks cannot have an assignee", apperror.ErrInvalid)
	}
	if err := validAssignee(in.AssigneeUserID); err != nil {
		return Task{}, err
	}
	if err := validDetails(in.ActivityID, in.Date); err != nil {
		return Task{}, err
	}
	return s.store.Create(ctx, userID, tripID, in)
}

func (s *Service) Update(ctx context.Context, userID, tripID, id int64, in UpdateInput) (Task, error) {
	if err := s.authz.RequireEditor(ctx, userID, tripID); err != nil {
		return Task{}, err
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return Task{}, err
	}
	in.Title = title
	if err := validDetails(in.ActivityID, in.Date); err != nil {
		return Task{}, err
	}
	return s.store.Update(ctx, tripID, id, in)
}

func (s *Service) Delete(ctx context.Context, userID, tripID, id int64) error {
	if err := s.authz.RequireEditor(ctx, userID, tripID); err != nil {
		return err
	}
	return s.store.Delete(ctx, tripID, id)
}

func (s *Service) Assign(ctx context.Context, userID, tripID, id int64, in AssigneeInput) error {
	if err := s.authz.RequireEditor(ctx, userID, tripID); err != nil {
		return err
	}
	if err := validAssignee(in.UserID); err != nil {
		return err
	}
	return s.store.Assign(ctx, tripID, id, in.UserID)
}

func (s *Service) Claim(ctx context.Context, userID, tripID, id int64) error {
	if err := s.authz.RequireNonViewer(ctx, userID, tripID); err != nil {
		return err
	}
	return s.store.Claim(ctx, userID, tripID, id)
}

func (s *Service) Drop(ctx context.Context, userID, tripID, id int64) error {
	if err := s.authz.RequireNonViewer(ctx, userID, tripID); err != nil {
		return err
	}
	return s.store.Drop(ctx, userID, tripID, id)
}

func (s *Service) Complete(ctx context.Context, userID, tripID, id int64) error {
	if err := s.authz.RequireNonViewer(ctx, userID, tripID); err != nil {
		return err
	}
	return s.store.Complete(ctx, userID, tripID, id)
}

// Reopen undoes the caller's own completion. Owners and editors can also reopen
// another person's finished one-person task, but never another person's tick.
func (s *Service) Reopen(ctx context.Context, userID, tripID, id int64) error {
	if err := s.authz.RequireNonViewer(ctx, userID, tripID); err != nil {
		return err
	}
	err := s.authz.RequireEditor(ctx, userID, tripID)
	if err != nil && !errors.Is(err, apperror.ErrForbidden) {
		return err
	}
	return s.store.Reopen(ctx, userID, tripID, id, err == nil)
}
