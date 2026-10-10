package posts

import (
	"context"
	"sort"
	"sync"
	"time"
)

// fakeRepo is a hand-rolled in-memory Repository used for isolated service
// tests. It mirrors the MySQL adapter's WHERE id = ? AND author_id = ? guard:
// Update and Delete only touch a row the given author owns.
type fakeRepo struct {
	mu     sync.Mutex
	posts  map[int64]Post
	nextID int64

	createErr error
	getErr    error
	listErr   error
	updateErr error
	deleteErr error

	lastCreateAuthorID int64
	lastListPage       int
	lastListLimit      int
	listCalls          int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{posts: make(map[int64]Post), nextID: 1}
}

func (f *fakeRepo) Create(_ context.Context, authorID int64, title, content string) (Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createErr != nil {
		return Post{}, f.createErr
	}

	now := time.Now()
	p := Post{ID: f.nextID, AuthorID: authorID, Title: title, Content: content, CreatedAt: now, UpdatedAt: now}
	f.nextID++
	f.posts[p.ID] = p
	f.lastCreateAuthorID = authorID
	return p, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id int64) (Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.getErr != nil {
		return Post{}, f.getErr
	}
	p, ok := f.posts[id]
	if !ok {
		return Post{}, ErrNotFound
	}
	return p, nil
}

func (f *fakeRepo) List(_ context.Context, page, limit int) ([]Post, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	f.lastListPage, f.lastListLimit = page, limit
	f.listCalls++

	all := make([]Post, 0, len(f.posts))
	for _, p := range f.posts {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all, int64(len(all)), nil
}

func (f *fakeRepo) Update(_ context.Context, id, authorID int64, title, content string) (Post, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.updateErr != nil {
		return Post{}, f.updateErr
	}
	p, ok := f.posts[id]
	if !ok || p.AuthorID != authorID {
		return Post{}, ErrNotFound
	}
	p.Title, p.Content, p.UpdatedAt = title, content, time.Now()
	f.posts[id] = p
	return p, nil
}

func (f *fakeRepo) Delete(_ context.Context, id, authorID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.deleteErr != nil {
		return f.deleteErr
	}
	p, ok := f.posts[id]
	if !ok || p.AuthorID != authorID {
		return ErrNotFound
	}
	delete(f.posts, id)
	return nil
}

// seed stores a post directly, bypassing the service, so tests can set up an
// author's post without exercising Create.
func (f *fakeRepo) seed(authorID int64, title, content string) Post {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	p := Post{ID: f.nextID, AuthorID: authorID, Title: title, Content: content, CreatedAt: now, UpdatedAt: now}
	f.nextID++
	f.posts[p.ID] = p
	return p
}
