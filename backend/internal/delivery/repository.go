package delivery

import (
	"context"
	"errors"
	"sort"
	"sync"
)

var ErrNotFound = errors.New("delivery item not found")

type Repository interface {
	Create(context.Context, WorkItem) error
	Get(context.Context, string) (WorkItem, error)
	List(context.Context) ([]WorkItem, error)
	Save(context.Context, WorkItem) error
}

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]WorkItem
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]WorkItem)}
}

func (repository *MemoryRepository) Create(_ context.Context, item WorkItem) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.items[item.ID] = cloneWorkItem(item)
	return nil
}

func (repository *MemoryRepository) Get(_ context.Context, id string) (WorkItem, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	item, ok := repository.items[id]
	if !ok {
		return WorkItem{}, ErrNotFound
	}
	return cloneWorkItem(item), nil
}

func (repository *MemoryRepository) List(_ context.Context) ([]WorkItem, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	items := make([]WorkItem, 0, len(repository.items))
	for _, item := range repository.items {
		items = append(items, cloneWorkItem(item))
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].UpdatedAt.After(items[right].UpdatedAt)
	})
	return items, nil
}

func (repository *MemoryRepository) Save(_ context.Context, item WorkItem) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, ok := repository.items[item.ID]; !ok {
		return ErrNotFound
	}
	repository.items[item.ID] = cloneWorkItem(item)
	return nil
}

func cloneWorkItem(item WorkItem) WorkItem {
	item.Decisions = append([]Decision(nil), item.Decisions...)
	item.Evidence = append([]Evidence(nil), item.Evidence...)
	return item
}
