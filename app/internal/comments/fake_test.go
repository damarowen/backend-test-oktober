package comments

import (
	"context"
	"sort"
	"sync"
	"time"
)

// fakeRepo is a hand-rolled in-memory Repository used for isolated service
// tests. ListByPost filters by post id like the SQL adapter does.
type fakeRepo struct {
	mu     sync.Mutex
	stored map[int64]Comment
	nextID int64

	createErr error
	listErr   error

	createCalls int
	listCalls   int
	lastCreate  Comment
	lastPage    int
	lastLimit   int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{stored: make(map[int64]Comment), nextID: 1}
}

func (f *fakeRepo) Create(_ context.Context, c Comment) (Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createErr != nil {
		return Comment{}, f.createErr
	}
	f.createCalls++
	c.ID = f.nextID
	f.nextID++
	c.CreatedAt = time.Now()
	f.stored[c.ID] = c
	f.lastCreate = c
	return c, nil
}

func (f *fakeRepo) ListByPost(_ context.Context, postID int64, page, limit int) ([]Comment, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	f.listCalls++
	f.lastPage, f.lastLimit = page, limit

	all := make([]Comment, 0, len(f.stored))
	for _, c := range f.stored {
		if c.PostID == postID {
			all = append(all, c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, int64(len(all)), nil
}

// seed stores a comment directly, bypassing the service, for list tests.
func (f *fakeRepo) seed(postID, authorID int64, authorName, content string) Comment {
	f.mu.Lock()
	defer f.mu.Unlock()

	c := Comment{ID: f.nextID, PostID: postID, AuthorID: authorID, AuthorName: authorName, Content: content, CreatedAt: time.Now()}
	f.nextID++
	f.stored[c.ID] = c
	return c
}

// fakePosts is a stub PostChecker.
type fakePosts struct {
	exists bool
	err    error

	calls  int
	lastID int64
}

func (f *fakePosts) Exists(_ context.Context, postID int64) (bool, error) {
	f.calls++
	f.lastID = postID
	if f.err != nil {
		return false, f.err
	}
	return f.exists, nil
}

// fakeUsers is a stub UserNameProvider.
type fakeUsers struct {
	name string
	err  error

	calls  int
	lastID int64
}

func (f *fakeUsers) UserName(_ context.Context, userID int64) (string, error) {
	f.calls++
	f.lastID = userID
	if f.err != nil {
		return "", f.err
	}
	return f.name, nil
}
