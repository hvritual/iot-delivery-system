package delivery

import (
	"context"
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotFound                = errors.New("delivery item not found")
	ErrRevisionConflict        = errors.New("delivery item revision conflict")
	ErrInvalidExpectedRevision = errors.New("delivery item expected revision must be positive")
)

type Repository interface {
	Create(context.Context, WorkItem) error
	Get(context.Context, string) (WorkItem, error)
	List(context.Context) ([]WorkItem, error)
	Save(context.Context, WorkItem, int64) error
	CreateProject(context.Context, Project) error
	GetProject(context.Context, string) (Project, error)
	ListProjects(context.Context) ([]Project, error)
	SaveProject(context.Context, Project) error
	CreateRelease(context.Context, Release) error
	GetRelease(context.Context, string) (Release, error)
	ListReleases(context.Context) ([]Release, error)
	SaveRelease(context.Context, Release) error
	CreateSprint(context.Context, Sprint) error
	GetSprint(context.Context, string) (Sprint, error)
	ListSprints(context.Context) ([]Sprint, error)
	SaveSprint(context.Context, Sprint) error
	CreateMilestone(context.Context, Milestone) error
	GetMilestone(context.Context, string) (Milestone, error)
	ListMilestones(context.Context) ([]Milestone, error)
	SaveMilestone(context.Context, Milestone) error
	CreateSavedView(context.Context, SavedView) error
	ListSavedViews(context.Context, string) ([]SavedView, error)
}

type MemoryRepository struct {
	mu         sync.RWMutex
	items      map[string]WorkItem
	projects   map[string]Project
	releases   map[string]Release
	sprints    map[string]Sprint
	milestones map[string]Milestone
	views      map[string]SavedView
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		items:      make(map[string]WorkItem),
		projects:   make(map[string]Project),
		releases:   make(map[string]Release),
		sprints:    make(map[string]Sprint),
		milestones: make(map[string]Milestone),
		views:      make(map[string]SavedView),
	}
}

func (repository *MemoryRepository) Create(_ context.Context, item WorkItem) error {
	if item.Revision == 0 {
		item.Revision = 1
	}
	if item.Revision != 1 {
		return errors.New("new delivery item revision must be 1")
	}
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
		if items[left].UpdatedAt.Equal(items[right].UpdatedAt) {
			return items[left].ID < items[right].ID
		}
		return items[left].UpdatedAt.After(items[right].UpdatedAt)
	})
	return items, nil
}

func (repository *MemoryRepository) Save(_ context.Context, item WorkItem, expectedRevision int64) error {
	if expectedRevision <= 0 {
		return ErrInvalidExpectedRevision
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	stored, ok := repository.items[item.ID]
	if !ok {
		return ErrNotFound
	}
	if stored.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	if item.Revision != expectedRevision+1 {
		return errors.New("delivery item revision must increment exactly once")
	}
	repository.items[item.ID] = cloneWorkItem(item)
	return nil
}

func (repository *MemoryRepository) CreateProject(_ context.Context, project Project) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.projects[project.ID]; exists {
		return errors.New("delivery project already exists")
	}
	repository.projects[project.ID] = project
	return nil
}

func (repository *MemoryRepository) GetProject(_ context.Context, id string) (Project, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	project, ok := repository.projects[id]
	if !ok {
		return Project{}, ErrNotFound
	}
	return project, nil
}

func (repository *MemoryRepository) ListProjects(_ context.Context) ([]Project, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	projects := make([]Project, 0, len(repository.projects))
	for _, project := range repository.projects {
		projects = append(projects, project)
	}
	sort.Slice(projects, func(left, right int) bool {
		if projects[left].UpdatedAt.Equal(projects[right].UpdatedAt) {
			return projects[left].ID < projects[right].ID
		}
		return projects[left].UpdatedAt.After(projects[right].UpdatedAt)
	})
	return projects, nil
}

func (repository *MemoryRepository) SaveProject(_ context.Context, project Project) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, ok := repository.projects[project.ID]; !ok {
		return ErrNotFound
	}
	repository.projects[project.ID] = project
	return nil
}

func (repository *MemoryRepository) CreateRelease(_ context.Context, release Release) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.releases[release.ID]; exists {
		return errors.New("delivery release already exists")
	}
	repository.releases[release.ID] = release
	return nil
}

func (repository *MemoryRepository) GetRelease(_ context.Context, id string) (Release, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	release, ok := repository.releases[id]
	if !ok {
		return Release{}, ErrNotFound
	}
	return release, nil
}

func (repository *MemoryRepository) ListReleases(_ context.Context) ([]Release, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	releases := make([]Release, 0, len(repository.releases))
	for _, release := range repository.releases {
		releases = append(releases, release)
	}
	sort.Slice(releases, func(left, right int) bool {
		if releases[left].UpdatedAt.Equal(releases[right].UpdatedAt) {
			return releases[left].ID < releases[right].ID
		}
		return releases[left].UpdatedAt.After(releases[right].UpdatedAt)
	})
	return releases, nil
}

func (repository *MemoryRepository) SaveRelease(_ context.Context, release Release) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, ok := repository.releases[release.ID]; !ok {
		return ErrNotFound
	}
	repository.releases[release.ID] = release
	return nil
}

func (repository *MemoryRepository) CreateSprint(_ context.Context, sprint Sprint) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.sprints[sprint.ID]; exists {
		return errors.New("delivery sprint already exists")
	}
	repository.sprints[sprint.ID] = sprint
	return nil
}

func (repository *MemoryRepository) GetSprint(_ context.Context, id string) (Sprint, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	sprint, ok := repository.sprints[id]
	if !ok {
		return Sprint{}, ErrNotFound
	}
	return sprint, nil
}

func (repository *MemoryRepository) ListSprints(_ context.Context) ([]Sprint, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	sprints := make([]Sprint, 0, len(repository.sprints))
	for _, sprint := range repository.sprints {
		sprints = append(sprints, sprint)
	}
	sort.Slice(sprints, func(left, right int) bool {
		if sprints[left].UpdatedAt.Equal(sprints[right].UpdatedAt) {
			return sprints[left].ID < sprints[right].ID
		}
		return sprints[left].UpdatedAt.After(sprints[right].UpdatedAt)
	})
	return sprints, nil
}

func (repository *MemoryRepository) SaveSprint(_ context.Context, sprint Sprint) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, ok := repository.sprints[sprint.ID]; !ok {
		return ErrNotFound
	}
	repository.sprints[sprint.ID] = sprint
	return nil
}

func (repository *MemoryRepository) CreateMilestone(_ context.Context, milestone Milestone) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.milestones[milestone.ID]; exists {
		return errors.New("delivery milestone already exists")
	}
	repository.milestones[milestone.ID] = milestone
	return nil
}

func (repository *MemoryRepository) GetMilestone(_ context.Context, id string) (Milestone, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	milestone, ok := repository.milestones[id]
	if !ok {
		return Milestone{}, ErrNotFound
	}
	return milestone, nil
}

func (repository *MemoryRepository) ListMilestones(_ context.Context) ([]Milestone, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	milestones := make([]Milestone, 0, len(repository.milestones))
	for _, milestone := range repository.milestones {
		milestones = append(milestones, milestone)
	}
	sort.Slice(milestones, func(left, right int) bool {
		if milestones[left].UpdatedAt.Equal(milestones[right].UpdatedAt) {
			return milestones[left].ID < milestones[right].ID
		}
		return milestones[left].UpdatedAt.After(milestones[right].UpdatedAt)
	})
	return milestones, nil
}

func (repository *MemoryRepository) SaveMilestone(_ context.Context, milestone Milestone) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, ok := repository.milestones[milestone.ID]; !ok {
		return ErrNotFound
	}
	repository.milestones[milestone.ID] = milestone
	return nil
}

func (repository *MemoryRepository) CreateSavedView(_ context.Context, view SavedView) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.views[view.ID]; exists {
		return errors.New("delivery saved view already exists")
	}
	repository.views[view.ID] = view
	return nil
}

func (repository *MemoryRepository) ListSavedViews(_ context.Context, owner string) ([]SavedView, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	views := make([]SavedView, 0, len(repository.views))
	for _, view := range repository.views {
		if owner == "" || view.Owner == owner {
			views = append(views, view)
		}
	}
	sort.Slice(views, func(left, right int) bool {
		if views[left].UpdatedAt.Equal(views[right].UpdatedAt) {
			return views[left].ID < views[right].ID
		}
		return views[left].UpdatedAt.After(views[right].UpdatedAt)
	})
	return views, nil
}

func cloneWorkItem(item WorkItem) WorkItem {
	item.Decisions = append([]Decision(nil), item.Decisions...)
	item.Evidence = append([]Evidence(nil), item.Evidence...)
	item.Dependencies = append([]WorkItemDependency(nil), item.Dependencies...)
	item.IoTBindings = append([]IoTBinding(nil), item.IoTBindings...)
	for index := range item.IoTBindings {
		item.IoTBindings[index].Attributes = cloneStringMap(item.IoTBindings[index].Attributes)
	}
	item.TraceLinks = append([]TraceLink(nil), item.TraceLinks...)
	item.Comments = append([]Comment(nil), item.Comments...)
	item.Activities = append([]Activity(nil), item.Activities...)
	return item
}
