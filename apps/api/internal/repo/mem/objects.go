package mem

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"doelab/api/internal/domain"
	"doelab/api/internal/service"
)

// Objects is an in-memory service.ObjectStore.
type Objects struct {
	mu      sync.Mutex
	objects map[string]Object
}

// Object is one stored object.
type Object struct {
	ContentType string
	Body        []byte
}

// NewObjects returns an empty object store.
func NewObjects() *Objects {
	return &Objects{objects: map[string]Object{}}
}

var _ service.ObjectStore = (*Objects)(nil)

// Put stores body under key.
func (o *Objects) Put(_ context.Context, key, contentType string, body []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.objects[key] = Object{ContentType: contentType, Body: append([]byte(nil), body...)}
	return nil
}

// Get returns the object at key.
func (o *Objects) Get(_ context.Context, key string) ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	obj, ok := o.objects[key]
	if !ok {
		return nil, fmt.Errorf("object %s: %w", key, domain.ErrNotFound)
	}
	return obj.Body, nil
}

// PresignGet returns a URL that names the key and the lifetime. Nothing
// serves it; a test checks what was asked for.
func (o *Objects) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	return "https://objects.test/" + url.PathEscape(key) + "?expires=" + ttl.String(), nil
}

// Stat returns the object at key as it was stored.
func (o *Objects) Stat(key string) (Object, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	obj, ok := o.objects[key]
	return obj, ok
}
