package comments

import (
	"context"
	"errors"
	"strings"
	"testing"

	"app/internal/identity"
)

const validContent = "Nice post!"

func TestCreate_Success(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{exists: true}
	users := &fakeUsers{name: "Budi"}
	svc := NewService(repo, posts, users)

	got, err := svc.Create(context.Background(), 10, 7, "  "+validContent+"  ")
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	if got.PostID != 10 {
		t.Errorf("PostID: got %d, want 10", got.PostID)
	}
	if got.AuthorID != 7 {
		t.Errorf("AuthorID: got %d, want the authenticated user 7", got.AuthorID)
	}
	if got.AuthorName != "Budi" {
		t.Errorf("AuthorName: got %q, want the provider snapshot %q", got.AuthorName, "Budi")
	}
	if got.Content != validContent {
		t.Errorf("Content: got %q, want trimmed %q", got.Content, validContent)
	}
	if posts.lastID != 10 {
		t.Errorf("PostChecker received post %d, want 10", posts.lastID)
	}
	if users.lastID != 7 {
		t.Errorf("UserNameProvider received user %d, want 7", users.lastID)
	}
	if repo.createCalls != 1 {
		t.Errorf("repo Create calls: got %d, want 1", repo.createCalls)
	}
}

func TestCreate_InvalidContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"too long", strings.Repeat("a", 65536)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			posts := &fakePosts{exists: true}
			svc := NewService(repo, posts, &fakeUsers{name: "Budi"})

			_, err := svc.Create(context.Background(), 1, 1, tt.content)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("got %v, want ErrInvalidInput", err)
			}
			if strings.Contains(strings.ToLower(err.Error()), "byte") {
				t.Errorf("user-facing message %q must not use byte wording", err.Error())
			}
			if posts.calls != 0 || repo.createCalls != 0 {
				t.Error("invalid content must be rejected before any downstream call")
			}
		})
	}
}

func TestCreate_PostNotFound(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &fakePosts{exists: false}, &fakeUsers{name: "Budi"})

	_, err := svc.Create(context.Background(), 999, 1, validContent)
	if !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("got %v, want ErrPostNotFound", err)
	}
	if repo.createCalls != 0 {
		t.Error("the comment must not be stored when the post is missing")
	}
}

func TestCreate_AuthorNotFound(t *testing.T) {
	svc := NewService(newFakeRepo(), &fakePosts{exists: true}, &fakeUsers{err: identity.ErrUserNotFound})

	_, err := svc.Create(context.Background(), 1, 999, validContent)
	if !errors.Is(err, ErrAuthorNotFound) {
		t.Errorf("got %v, want ErrAuthorNotFound", err)
	}
}

// TestCreate_FKRace_Propagates simulates the lost insert race: the post is
// deleted between the existence check and the insert, and the adapter turns the
// MySQL foreign-key violation into ErrPostNotFound. The service must propagate
// that sentinel unchanged — and no MySQL detail may ride along.
func TestCreate_FKRace_Propagates(t *testing.T) {
	repo := newFakeRepo()
	repo.createErr = ErrPostNotFound
	svc := NewService(repo, &fakePosts{exists: true}, &fakeUsers{name: "Budi"})

	_, err := svc.Create(context.Background(), 1, 1, validContent)

	if !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("got %v, want ErrPostNotFound", err)
	}
	if strings.Contains(err.Error(), "1452") {
		t.Errorf("client-facing error %q must not contain MySQL detail", err.Error())
	}
}

// TestCreate_InfraErrors_Propagate pins that outages are never misreported as
// business conditions (404/400): only the identity "user not found" sentinel
// becomes ErrAuthorNotFound; anything else surfaces unchanged as a 500.
func TestCreate_InfraErrors_Propagate(t *testing.T) {
	infra := errors.New("dial tcp 127.0.0.1:3306: connect: connection refused")

	tests := []struct {
		name   string
		mutate func(repo *fakeRepo, posts *fakePosts, users *fakeUsers)
	}{
		{"post checker outage", func(_ *fakeRepo, posts *fakePosts, _ *fakeUsers) { posts.err = infra }},
		{"user provider outage", func(_ *fakeRepo, _ *fakePosts, users *fakeUsers) { users.err = infra }},
		{"repo outage", func(repo *fakeRepo, _ *fakePosts, _ *fakeUsers) { repo.createErr = infra }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			posts := &fakePosts{exists: true}
			users := &fakeUsers{name: "Budi"}
			tt.mutate(repo, posts, users)

			_, err := NewService(repo, posts, users).Create(context.Background(), 1, 1, validContent)
			if !errors.Is(err, infra) {
				t.Fatalf("got %v, want the infrastructure error unchanged", err)
			}
			if errors.Is(err, ErrPostNotFound) || errors.Is(err, ErrAuthorNotFound) || errors.Is(err, ErrInvalidInput) {
				t.Error("an infrastructure failure must not be reported as a business condition")
			}
		})
	}
}

func TestListByPost_PostNotFound(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &fakePosts{exists: false}, &fakeUsers{})

	_, _, err := svc.ListByPost(context.Background(), 999, 0, 0)
	if !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("got %v, want ErrPostNotFound", err)
	}
	if repo.listCalls != 0 {
		t.Error("comments must not be queried when the post is missing")
	}
}

func TestListByPost_Empty(t *testing.T) {
	svc := NewService(newFakeRepo(), &fakePosts{exists: true}, &fakeUsers{})

	list, total, err := svc.ListByPost(context.Background(), 1, 0, 0)
	if err != nil {
		t.Fatalf("ListByPost() unexpected error: %v", err)
	}
	if list == nil {
		t.Error("list must not be nil (JSON must marshal [])")
	}
	if total != 0 {
		t.Errorf("total: got %d, want 0", total)
	}
}

func TestListByPost_Defaults(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, &fakePosts{exists: true}, &fakeUsers{})

	_, _, err := svc.ListByPost(context.Background(), 5, 0, 0)
	if err != nil {
		t.Fatalf("ListByPost() unexpected error: %v", err)
	}
	if repo.lastPage != 1 || repo.lastLimit != 10 {
		t.Errorf("repo called with page=%d limit=%d, want defaults 1/10", repo.lastPage, repo.lastLimit)
	}
}

func TestListByPost_InvalidPagination(t *testing.T) {
	tests := []struct {
		name    string
		page    int
		limit   int
		wantErr bool
	}{
		{"negative page", -1, 10, true},
		{"limit over max", 1, 101, true},
		{"negative limit", 1, -5, true},
		{"zero values default", 0, 0, false},
		{"max limit accepted", 1, 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			posts := &fakePosts{exists: true}
			svc := NewService(repo, posts, &fakeUsers{})

			_, _, err := svc.ListByPost(context.Background(), 1, tt.page, tt.limit)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("got %v, want ErrInvalidInput", err)
				}
				if posts.calls != 0 || repo.listCalls != 0 {
					t.Error("out-of-range pagination must be rejected before any downstream call")
				}
				return
			}
			if err != nil {
				t.Fatalf("ListByPost() unexpected error: %v", err)
			}
		})
	}
}
