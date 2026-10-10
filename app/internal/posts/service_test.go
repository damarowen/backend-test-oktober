package posts

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

const (
	validTitle   = "Hello World"
	validContent = "This is the post body."
)

func TestCreate_Success(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)

	p, err := svc.Create(context.Background(), 7, "  "+validTitle+"  ", validContent)
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
	if p.ID <= 0 {
		t.Errorf("ID: got %d, want > 0", p.ID)
	}
	// The author is taken from the authenticated user argument; the method has
	// no author parameter the caller could spoof through a request body.
	if p.AuthorID != 7 {
		t.Errorf("AuthorID: got %d, want the authenticated user 7", p.AuthorID)
	}
	if repo.lastCreateAuthorID != 7 {
		t.Errorf("repo received authorID %d, want 7", repo.lastCreateAuthorID)
	}
	if p.Title != validTitle {
		t.Errorf("Title: got %q, want trimmed %q", p.Title, validTitle)
	}
	if p.Content != validContent {
		t.Errorf("Content: got %q, want %q", p.Content, validContent)
	}
}

func TestCreate_Boundaries(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)

	maxTitle := strings.Repeat("a", maxTitleLength)
	maxContent := strings.Repeat("a", maxContentLength)
	if _, err := svc.Create(context.Background(), 1, maxTitle, maxContent); err != nil {
		t.Errorf("exactly max title/content: got %v, want success", err)
	}
}

func TestCreate_InvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		content string
	}{
		{"empty title", "", validContent},
		{"whitespace title", "   ", validContent},
		{"title too long", strings.Repeat("a", maxTitleLength+1), validContent},
		{"empty content", validTitle, ""},
		{"content too long", validTitle, strings.Repeat("a", maxContentLength+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			svc := NewService(repo)

			_, err := svc.Create(context.Background(), 1, tt.title, tt.content)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("got %v, want ErrInvalidInput", err)
			}
			if strings.Contains(strings.ToLower(err.Error()), "byte") {
				t.Errorf("user-facing message %q must not use byte wording", err.Error())
			}
		})
	}
}

func TestGet_Found(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	seed := repo.seed(1, "Title", "Body")

	p, err := svc.Get(context.Background(), seed.ID)
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if p.ID != seed.ID {
		t.Errorf("ID: got %d, want %d", p.ID, seed.ID)
	}
}

func TestGet_NotFound(t *testing.T) {
	svc := NewService(newFakeRepo())

	_, err := svc.Get(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestList_Defaults(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)

	list, total, err := svc.List(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if repo.lastListPage != 1 || repo.lastListLimit != 10 {
		t.Errorf("repo called with page=%d limit=%d, want defaults 1/10", repo.lastListPage, repo.lastListLimit)
	}
	if list == nil {
		t.Error("list must not be nil")
	}
	if total != 0 {
		t.Errorf("total: got %d, want 0", total)
	}
}

func TestList_InvalidPagination(t *testing.T) {
	tests := []struct {
		name      string
		page      int
		limit     int
		wantErr   bool
		wantPage  int
		wantLimit int
	}{
		{"negative page", -1, 10, true, 0, 0},
		{"zero limit defaults", 1, 0, false, 1, 10},
		{"limit over max", 1, 101, true, 0, 0},
		{"negative limit", 1, -5, true, 0, 0},
		{"zero page defaults", 0, 5, false, 1, 5},
		{"max limit accepted", 3, 100, false, 3, 100},
		{"huge page would overflow the offset", math.MaxInt64, 100, true, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			svc := NewService(repo)

			_, _, err := svc.List(context.Background(), tt.page, tt.limit)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("got %v, want ErrInvalidInput", err)
				}
				if repo.listCalls != 0 {
					t.Error("repo must not be called when pagination is out of range")
				}
				return
			}
			if err != nil {
				t.Fatalf("List() unexpected error: %v", err)
			}
			if repo.lastListPage != tt.wantPage || repo.lastListLimit != tt.wantLimit {
				t.Errorf("repo called with page=%d limit=%d, want %d/%d",
					repo.lastListPage, repo.lastListLimit, tt.wantPage, tt.wantLimit)
			}
		})
	}
}

func TestUpdate_ByOwner_Succeeds(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	ctx := context.Background()
	seed := repo.seed(1, "Original", "Original body")

	updated, err := svc.Update(ctx, 1, seed.ID, "  Updated  ", "Updated body")
	if err != nil {
		t.Fatalf("Update() unexpected error: %v", err)
	}
	if updated.AuthorID != 1 {
		t.Errorf("AuthorID: got %d, want 1 (the author never changes)", updated.AuthorID)
	}
	if updated.Title != "Updated" {
		t.Errorf("Title: got %q, want trimmed %q", updated.Title, "Updated")
	}
	if updated.Content != "Updated body" {
		t.Errorf("Content: got %q, want %q", updated.Content, "Updated body")
	}
}

func TestUpdate_ByAnotherUser_Forbidden(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	ctx := context.Background()
	seed := repo.seed(1, "Original", "Original body")

	_, err := svc.Update(ctx, 2, seed.ID, "Hijacked", "Hijacked body")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden (403, not 404)", err)
	}

	stored, err := repo.GetByID(ctx, seed.ID)
	if err != nil {
		t.Fatalf("GetByID() unexpected error: %v", err)
	}
	if stored.Title != "Original" || stored.Content != "Original body" {
		t.Error("a forbidden update must not modify the post")
	}
}

func TestUpdate_NotFound(t *testing.T) {
	svc := NewService(newFakeRepo())

	_, err := svc.Update(context.Background(), 1, 999, "Title", "Body")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestUpdate_InvalidInput(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	seed := repo.seed(1, "Original", "Original body")

	_, err := svc.Update(context.Background(), 1, seed.ID, "", "Body")
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("got %v, want ErrInvalidInput", err)
	}
}

func TestDelete_ByOwner_Succeeds(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	ctx := context.Background()
	seed := repo.seed(1, "Title", "Body")

	if err := svc.Delete(ctx, 1, seed.ID); err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	if _, err := repo.GetByID(ctx, seed.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("post still present after delete (got %v)", err)
	}
}

func TestDelete_ByAnotherUser_Forbidden(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	ctx := context.Background()
	seed := repo.seed(1, "Title", "Body")

	if err := svc.Delete(ctx, 2, seed.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden (403, not 404)", err)
	}
	if _, err := repo.GetByID(ctx, seed.ID); err != nil {
		t.Errorf("a forbidden delete must not remove the post: %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	svc := NewService(newFakeRepo())

	err := svc.Delete(context.Background(), 1, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestExists_True(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	seed := repo.seed(1, "Title", "Body")

	ok, err := svc.Exists(context.Background(), seed.ID)
	if err != nil {
		t.Fatalf("Exists() unexpected error: %v", err)
	}
	if !ok {
		t.Error("Exists() = false, want true for a stored post")
	}
}

func TestExists_False(t *testing.T) {
	svc := NewService(newFakeRepo())

	ok, err := svc.Exists(context.Background(), 999)
	if err != nil {
		t.Fatalf("Exists() unexpected error: %v", err)
	}
	if ok {
		t.Error("Exists() = true, want false for a missing post")
	}
}

func TestExists_RepositoryError_Propagates(t *testing.T) {
	repo := newFakeRepo()
	repo.getErr = errors.New("db down")
	svc := NewService(repo)

	ok, err := svc.Exists(context.Background(), 1)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("infrastructure error must propagate (got %v), not be read as a missing post", err)
	}
	if ok {
		t.Error("Exists() = true on an infrastructure error")
	}
}
