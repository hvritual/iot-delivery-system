package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/hvritual/yunka.io/framework/core/identity"
)

var (
	ErrEvidenceRequired                 = errors.New("gate advancement requires at least one evidence record")
	ErrInvalidGateTransition            = errors.New("invalid delivery gate transition")
	ErrRetrospectiveRequired            = errors.New("closing a delivery item requires a retrospective")
	ErrReleaseNotValidated              = errors.New("delivery item must pass production validation before it can close")
	ErrProjectParentMismatch            = errors.New("delivery item parent must belong to the same project")
	ErrDuplicateWorkItem                = errors.New("duplicate delivery work item")
	ErrInvalidDependency                = errors.New("invalid delivery work item dependency")
	ErrCircularDependency               = errors.New("delivery work item dependency creates a cycle")
	ErrInvalidIoTBinding                = errors.New("invalid IoT binding")
	ErrInvalidTraceLink                 = errors.New("invalid delivery trace link")
	ErrInvalidWorkItemUpdate            = errors.New("delivery work item update has no editable fields")
	ErrPlanningScopeMismatch            = errors.New("delivery planning record must belong to the same project")
	ErrImplementationPrincipalRequired  = errors.New("delivery implementation requires a trusted principal")
	ErrProductionPrincipalRequired      = errors.New("production validation and closing require a named JWT principal")
	ErrImplementationSourceRequired     = errors.New("delivery item has no trusted implementation principal")
	ErrImplementerCannotVerifyOwnChange = errors.New("implementer cannot production-verify or close their own change")
	ErrCanonicalUserRequired            = errors.New("saved views require a canonical user identity")
)

type Exporter interface {
	Export(context.Context, []WorkItem) error
}

type Service struct {
	repository Repository
	exporter   Exporter
	stager     MutationStager
	now        func() time.Time
	sequence   atomic.Uint64
}

func NewService(repository Repository, exporter Exporter, stagers ...MutationStager) *Service {
	var stager MutationStager
	for _, candidate := range stagers {
		if candidate != nil {
			stager = candidate
			break
		}
	}
	return &Service{repository: repository, exporter: exporter, stager: stager, now: time.Now}
}

func (service *Service) CreateProject(ctx context.Context, input ProjectInput) (Project, error) {
	if service == nil || service.repository == nil {
		return Project{}, errors.New("delivery service is not configured")
	}
	if strings.TrimSpace(input.Name) == "" {
		return Project{}, errors.New("delivery project name is required")
	}
	if strings.TrimSpace(string(input.Board)) == "" {
		return Project{}, errors.New("delivery project board is required")
	}
	if strings.TrimSpace(input.Owner) == "" {
		return Project{}, errors.New("delivery project owner is required")
	}
	now := service.now().UTC()
	id, err := service.nextProjectID(ctx, now)
	if err != nil {
		return Project{}, err
	}
	project := Project{
		ID:             id,
		OrganizationID: strings.TrimSpace(input.OrganizationID),
		Name:           strings.TrimSpace(input.Name),
		Board:          input.Board,
		Owner:          strings.TrimSpace(input.Owner),
		Description:    strings.TrimSpace(input.Description),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := service.persistProjectMutation(ctx, "delivery.project.created", project, func() error {
		return service.repository.CreateProject(ctx, project)
	}); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (service *Service) CreateRelease(ctx context.Context, input ReleaseInput) (Release, error) {
	if service == nil || service.repository == nil {
		return Release{}, errors.New("delivery service is not configured")
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	if input.ProjectID == "" {
		return Release{}, errors.New("delivery release project ID is required")
	}
	if _, err := service.repository.GetProject(ctx, input.ProjectID); err != nil {
		return Release{}, fmt.Errorf("get delivery project: %w", err)
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Version) == "" {
		return Release{}, errors.New("delivery release name and version are required")
	}
	targetDate, err := normalizeDate(input.TargetDate, "delivery release target date", false)
	if err != nil {
		return Release{}, err
	}
	now := service.now().UTC()
	id, err := service.nextReleaseID(ctx, now)
	if err != nil {
		return Release{}, err
	}
	release := Release{
		ID:          id,
		ProjectID:   input.ProjectID,
		Name:        strings.TrimSpace(input.Name),
		Version:     strings.TrimSpace(input.Version),
		Status:      strings.TrimSpace(input.Status),
		TargetDate:  targetDate,
		Description: strings.TrimSpace(input.Description),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := service.persistProjectMutation(ctx, "delivery.release.created", Project{ID: release.ID, UpdatedAt: now}, func() error {
		return service.repository.CreateRelease(ctx, release)
	}); err != nil {
		return Release{}, err
	}
	return release, nil
}

func (service *Service) CreateSprint(ctx context.Context, input SprintInput) (Sprint, error) {
	if service == nil || service.repository == nil {
		return Sprint{}, errors.New("delivery service is not configured")
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	if input.ProjectID == "" {
		return Sprint{}, errors.New("delivery sprint project ID is required")
	}
	if _, err := service.repository.GetProject(ctx, input.ProjectID); err != nil {
		return Sprint{}, fmt.Errorf("get delivery project: %w", err)
	}
	if strings.TrimSpace(input.Name) == "" {
		return Sprint{}, errors.New("delivery sprint name is required")
	}
	startDate, err := normalizeDate(input.StartDate, "delivery sprint start date", true)
	if err != nil {
		return Sprint{}, err
	}
	endDate, err := normalizeDate(input.EndDate, "delivery sprint end date", true)
	if err != nil {
		return Sprint{}, err
	}
	if endDate < startDate {
		return Sprint{}, errors.New("delivery sprint end date must not be before start date")
	}
	now := service.now().UTC()
	id, err := service.nextSprintID(ctx, now)
	if err != nil {
		return Sprint{}, err
	}
	sprint := Sprint{
		ID:        id,
		ProjectID: input.ProjectID,
		Name:      strings.TrimSpace(input.Name),
		Goal:      strings.TrimSpace(input.Goal),
		StartDate: startDate,
		EndDate:   endDate,
		Status:    strings.TrimSpace(input.Status),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := service.persistProjectMutation(ctx, "delivery.sprint.created", Project{ID: sprint.ID, UpdatedAt: now}, func() error {
		return service.repository.CreateSprint(ctx, sprint)
	}); err != nil {
		return Sprint{}, err
	}
	return sprint, nil
}

func (service *Service) CreateMilestone(ctx context.Context, input MilestoneInput) (Milestone, error) {
	if service == nil || service.repository == nil {
		return Milestone{}, errors.New("delivery service is not configured")
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	if input.ProjectID == "" {
		return Milestone{}, errors.New("delivery milestone project ID is required")
	}
	if _, err := service.repository.GetProject(ctx, input.ProjectID); err != nil {
		return Milestone{}, fmt.Errorf("get delivery project: %w", err)
	}
	if strings.TrimSpace(input.Name) == "" {
		return Milestone{}, errors.New("delivery milestone name is required")
	}
	targetDate, err := normalizeDate(input.TargetDate, "delivery milestone target date", true)
	if err != nil {
		return Milestone{}, err
	}
	now := service.now().UTC()
	id, err := service.nextMilestoneID(ctx, now)
	if err != nil {
		return Milestone{}, err
	}
	milestone := Milestone{
		ID:          id,
		ProjectID:   input.ProjectID,
		Name:        strings.TrimSpace(input.Name),
		TargetDate:  targetDate,
		Status:      strings.TrimSpace(input.Status),
		Description: strings.TrimSpace(input.Description),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := service.persistProjectMutation(ctx, "delivery.milestone.created", Project{ID: milestone.ID, UpdatedAt: now}, func() error {
		return service.repository.CreateMilestone(ctx, milestone)
	}); err != nil {
		return Milestone{}, err
	}
	return milestone, nil
}

func (service *Service) Create(ctx context.Context, input CreateInput) (WorkItem, error) {
	if service == nil || service.repository == nil {
		return WorkItem{}, errors.New("delivery service is not configured")
	}
	if strings.TrimSpace(input.Title) == "" {
		return WorkItem{}, errors.New("delivery item title is required")
	}
	if strings.TrimSpace(string(input.Board)) == "" {
		return WorkItem{}, errors.New("delivery item board is required")
	}
	if strings.TrimSpace(input.Owner) == "" {
		return WorkItem{}, errors.New("delivery item owner is required")
	}
	if input.Priority == "" {
		input.Priority = PriorityP1
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.ParentID = strings.TrimSpace(input.ParentID)
	if input.ProjectID != "" {
		if _, err := service.repository.GetProject(ctx, input.ProjectID); err != nil {
			return WorkItem{}, fmt.Errorf("get delivery project: %w", err)
		}
	}
	if input.ParentID != "" {
		parent, err := service.repository.Get(ctx, input.ParentID)
		if err != nil {
			return WorkItem{}, fmt.Errorf("get delivery item parent: %w", err)
		}
		if input.ProjectID == "" || parent.ProjectID != input.ProjectID {
			return WorkItem{}, ErrProjectParentMismatch
		}
	}
	if input.Kind == "" {
		input.Kind = WorkItemKindTask
	}
	input.ReleaseID = strings.TrimSpace(input.ReleaseID)
	input.SprintID = strings.TrimSpace(input.SprintID)
	input.MilestoneID = strings.TrimSpace(input.MilestoneID)
	if err := service.validatePlanningLinks(ctx, input.ProjectID, input.ReleaseID, input.SprintID, input.MilestoneID); err != nil {
		return WorkItem{}, err
	}
	if input.EstimatePoints < 0 {
		return WorkItem{}, errors.New("delivery estimate points cannot be negative")
	}
	if input.ProgressPercent < 0 || input.ProgressPercent > 100 {
		return WorkItem{}, errors.New("delivery progress percent must be between 0 and 100")
	}
	dependencies, err := service.validateDependencies(ctx, input.ProjectID, "", input.Dependencies)
	if err != nil {
		return WorkItem{}, err
	}
	if err := service.rejectExactDuplicate(ctx, input); err != nil {
		return WorkItem{}, err
	}
	now := service.now().UTC()
	bindings, err := normalizeIoTBindings(input.IoTBindings)
	if err != nil {
		return WorkItem{}, err
	}
	traceLinks, err := normalizeTraceLinks(input.TraceLinks, now)
	if err != nil {
		return WorkItem{}, err
	}
	id, err := service.nextID(ctx, now)
	if err != nil {
		return WorkItem{}, err
	}
	item := WorkItem{
		Revision:        1,
		ID:              id,
		Title:           strings.TrimSpace(input.Title),
		Board:           input.Board,
		ProjectID:       input.ProjectID,
		ParentID:        input.ParentID,
		Kind:            input.Kind,
		Dependencies:    dependencies,
		Type:            strings.TrimSpace(input.Type),
		Owner:           strings.TrimSpace(input.Owner),
		Priority:        input.Priority,
		Status:          StatusPlanned,
		Gate:            GatePlanning,
		ReleaseID:       strings.TrimSpace(input.ReleaseID),
		SprintID:        strings.TrimSpace(input.SprintID),
		MilestoneID:     strings.TrimSpace(input.MilestoneID),
		StartDate:       strings.TrimSpace(input.StartDate),
		DueDate:         strings.TrimSpace(input.DueDate),
		EstimatePoints:  input.EstimatePoints,
		ProgressPercent: input.ProgressPercent,
		Plan:            strings.TrimSpace(input.Plan),
		Solution:        strings.TrimSpace(input.Solution),
		IoTBindings:     bindings,
		TraceLinks:      traceLinks,
		IsSample:        input.IsSample,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	item.Activities = append(item.Activities, service.newActivity(ctx, item, "created", "创建交付事项", now))
	if err := service.persistMutation(ctx, "delivery.work-item.created", item, func() error {
		return service.repository.Create(ctx, item)
	}); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (service *Service) validateDependencies(ctx context.Context, projectID, itemID string, dependencies []WorkItemDependency) ([]WorkItemDependency, error) {
	if len(dependencies) == 0 {
		return nil, nil
	}
	if projectID == "" {
		return nil, ErrInvalidDependency
	}
	result := make([]WorkItemDependency, 0, len(dependencies))
	seen := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		dependency.ItemID = strings.TrimSpace(dependency.ItemID)
		if dependency.Relation == "" {
			dependency.Relation = DependencyDependsOn
		}
		if dependency.ItemID == "" || !isDependencyRelation(dependency.Relation) {
			return nil, ErrInvalidDependency
		}
		if itemID != "" && dependency.ItemID == itemID {
			return nil, ErrCircularDependency
		}
		if _, exists := seen[dependency.ItemID]; exists {
			continue
		}
		target, err := service.repository.Get(ctx, dependency.ItemID)
		if err != nil {
			return nil, fmt.Errorf("get dependency target: %w", err)
		}
		if target.ProjectID != projectID {
			return nil, ErrProjectParentMismatch
		}
		seen[dependency.ItemID] = struct{}{}
		result = append(result, dependency)
	}
	if itemID != "" && service.wouldCreateDependencyCycle(ctx, itemID, result) {
		return nil, ErrCircularDependency
	}
	return result, nil
}

func isDependencyRelation(value DependencyRelation) bool {
	switch value {
	case DependencyDependsOn, DependencyBlocks, DependencyRelated:
		return true
	default:
		return false
	}
}

// wouldCreateDependencyCycle builds the prerequisite graph after the current
// item's pending update. "blocks" is stored in the opposite direction from
// "depends_on", so it is normalized before the graph is traversed.
func (service *Service) wouldCreateDependencyCycle(ctx context.Context, itemID string, dependencies []WorkItemDependency) bool {
	items, err := service.repository.List(ctx)
	if err != nil {
		return true
	}
	prerequisites := make(map[string][]string, len(items))
	for _, item := range items {
		current := item.Dependencies
		if item.ID == itemID {
			current = dependencies
		}
		for _, dependency := range current {
			switch dependency.Relation {
			case DependencyDependsOn:
				prerequisites[item.ID] = append(prerequisites[item.ID], dependency.ItemID)
			case DependencyBlocks:
				prerequisites[dependency.ItemID] = append(prerequisites[dependency.ItemID], item.ID)
			}
		}
	}
	visited := make(map[string]bool, len(prerequisites))
	visiting := make(map[string]bool, len(prerequisites))
	var hasCycle func(string) bool
	hasCycle = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = true
		for _, prerequisite := range prerequisites[id] {
			if hasCycle(prerequisite) {
				return true
			}
		}
		delete(visiting, id)
		visited[id] = true
		return false
	}
	for id := range prerequisites {
		if hasCycle(id) {
			return true
		}
	}
	return false
}

func (service *Service) rejectExactDuplicate(ctx context.Context, input CreateInput) error {
	return service.rejectExactDuplicateExcept(ctx, input, "")
}

func (service *Service) rejectExactDuplicateExcept(ctx context.Context, input CreateInput, excludeID string) error {
	items, err := service.repository.List(ctx)
	if err != nil {
		return fmt.Errorf("list delivery items for duplicate check: %w", err)
	}
	wantedTitle := normalizedWorkItemTitle(input.Title)
	for _, item := range items {
		if item.ID == excludeID {
			continue
		}
		if item.Kind != input.Kind || normalizedWorkItemTitle(item.Title) != wantedTitle {
			continue
		}
		if input.ProjectID != "" && item.ProjectID == input.ProjectID {
			return fmt.Errorf("%w: %s", ErrDuplicateWorkItem, item.ID)
		}
		if input.ProjectID == "" && item.ProjectID == "" && item.Board == input.Board {
			return fmt.Errorf("%w: %s", ErrDuplicateWorkItem, item.ID)
		}
	}
	return nil
}

func normalizedWorkItemTitle(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func workItemTitleSimilarity(left, right string) float64 {
	left = normalizedWorkItemTitle(left)
	right = normalizedWorkItemTitle(right)
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 1
	}
	leftTokens := titleTokens(left)
	rightTokens := titleTokens(right)
	intersection := 0
	for token := range leftTokens {
		if _, exists := rightTokens[token]; exists {
			intersection++
		}
	}
	union := len(leftTokens) + len(rightTokens) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func titleTokens(value string) map[string]struct{} {
	runes := []rune(value)
	tokens := make(map[string]struct{}, len(runes))
	if len(runes) == 1 {
		tokens[value] = struct{}{}
		return tokens
	}
	for index := 0; index < len(runes)-1; index++ {
		tokens[string(runes[index:index+2])] = struct{}{}
	}
	return tokens
}

func (service *Service) Get(ctx context.Context, id string) (WorkItem, error) {
	if service == nil || service.repository == nil {
		return WorkItem{}, errors.New("delivery service is not configured")
	}
	return service.repository.Get(ctx, strings.TrimSpace(id))
}

func (service *Service) ListProjects(ctx context.Context) ([]Project, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("delivery service is not configured")
	}
	return service.repository.ListProjects(ctx)
}

func (service *Service) ListReleases(ctx context.Context, projectID string) ([]Release, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("delivery service is not configured")
	}
	projectID = strings.TrimSpace(projectID)
	releases, err := service.repository.ListReleases(ctx)
	if err != nil || projectID == "" {
		return releases, err
	}
	result := make([]Release, 0, len(releases))
	for _, release := range releases {
		if release.ProjectID == projectID {
			result = append(result, release)
		}
	}
	return result, nil
}

func (service *Service) ListSprints(ctx context.Context, projectID string) ([]Sprint, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("delivery service is not configured")
	}
	projectID = strings.TrimSpace(projectID)
	sprints, err := service.repository.ListSprints(ctx)
	if err != nil || projectID == "" {
		return sprints, err
	}
	result := make([]Sprint, 0, len(sprints))
	for _, sprint := range sprints {
		if sprint.ProjectID == projectID {
			result = append(result, sprint)
		}
	}
	return result, nil
}

func (service *Service) ListMilestones(ctx context.Context, projectID string) ([]Milestone, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("delivery service is not configured")
	}
	projectID = strings.TrimSpace(projectID)
	milestones, err := service.repository.ListMilestones(ctx)
	if err != nil || projectID == "" {
		return milestones, err
	}
	result := make([]Milestone, 0, len(milestones))
	for _, milestone := range milestones {
		if milestone.ProjectID == projectID {
			result = append(result, milestone)
		}
	}
	return result, nil
}

func (service *Service) SaveView(ctx context.Context, input SavedViewInput) (SavedView, error) {
	if service == nil || service.repository == nil {
		return SavedView{}, errors.New("delivery service is not configured")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return SavedView{}, errors.New("delivery saved view name is required")
	}
	owner, err := canonicalUserIDFromContext(ctx)
	if err != nil {
		return SavedView{}, err
	}
	now := service.now().UTC()
	id, err := service.nextSavedViewID(ctx, now)
	if err != nil {
		return SavedView{}, err
	}
	view := SavedView{
		ID:        id,
		Name:      name,
		Owner:     owner,
		Filter:    normalizeWorkItemFilter(input.Filter),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := service.repository.CreateSavedView(ctx, view); err != nil {
		return SavedView{}, err
	}
	return view, nil
}

func (service *Service) ListSavedViews(ctx context.Context) ([]SavedView, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("delivery service is not configured")
	}
	owner, err := canonicalUserIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return service.repository.ListSavedViews(ctx, owner)
}

func canonicalUserIDFromContext(ctx context.Context) (string, error) {
	principal, ok := identity.FromContext(ctx)
	if !ok || !principal.Authenticated || strings.TrimSpace(principal.UserID) == "" {
		return "", ErrCanonicalUserRequired
	}
	return strings.TrimSpace(principal.UserID), nil
}

func (service *Service) MemberWeek(ctx context.Context, member, weekStart string) (MemberWeek, error) {
	if service == nil || service.repository == nil {
		return MemberWeek{}, errors.New("delivery service is not configured")
	}
	member = strings.TrimSpace(member)
	if member == "" {
		return MemberWeek{}, errors.New("delivery member is required")
	}
	start, err := weekStartDate(weekStart, service.now())
	if err != nil {
		return MemberWeek{}, err
	}
	end := start.AddDate(0, 0, 6)
	items, err := service.repository.List(ctx)
	if err != nil {
		return MemberWeek{}, err
	}
	result := MemberWeek{
		Member:    member,
		WeekStart: start.Format("2006-01-02"),
		WeekEnd:   end.Format("2006-01-02"),
		Items:     make([]WorkItem, 0),
	}
	for _, item := range items {
		if item.Owner != member || !itemTouchesWeek(item, start, end) {
			continue
		}
		result.Items = append(result.Items, item)
	}
	sort.Slice(result.Items, func(left, right int) bool {
		leftDate := earliestItemDate(result.Items[left])
		rightDate := earliestItemDate(result.Items[right])
		if leftDate == rightDate {
			return result.Items[left].ID < result.Items[right].ID
		}
		if leftDate == "" {
			return false
		}
		if rightDate == "" {
			return true
		}
		return leftDate < rightDate
	})
	return result, nil
}

func (service *Service) ProjectProgress(ctx context.Context, projectID string) (ProjectProgress, error) {
	if service == nil || service.repository == nil {
		return ProjectProgress{}, errors.New("delivery service is not configured")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectProgress{}, errors.New("delivery project ID is required")
	}
	if _, err := service.repository.GetProject(ctx, projectID); err != nil {
		return ProjectProgress{}, fmt.Errorf("get delivery project: %w", err)
	}
	items, err := service.repository.List(ctx)
	if err != nil {
		return ProjectProgress{}, err
	}
	progress := ProjectProgress{ProjectID: projectID}
	for _, item := range items {
		if item.ProjectID != projectID || item.Kind == WorkItemKindEpic {
			continue
		}
		weight := item.EstimatePoints
		if weight <= 0 {
			weight = 1
		}
		itemProgress := effectiveProgress(item)
		progress.TotalItems++
		progress.TotalWeight += weight
		progress.CompletedWeight += weight * itemProgress / 100
		if itemProgress >= 100 {
			progress.CompletedItems++
		}
	}
	if progress.TotalWeight > 0 {
		progress.ProgressPercent = progress.CompletedWeight / progress.TotalWeight * 100
	}
	return progress, nil
}

// ProjectSchedule turns the existing task plan into an explainable daily
// health view. It does not mutate delivery data, so callers can refresh it as
// frequently as the desktop cockpit needs.
func (service *Service) ProjectSchedule(ctx context.Context, projectID string) (ProjectSchedule, error) {
	if service == nil || service.repository == nil {
		return ProjectSchedule{}, errors.New("delivery service is not configured")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectSchedule{}, errors.New("delivery project ID is required")
	}
	if _, err := service.repository.GetProject(ctx, projectID); err != nil {
		return ProjectSchedule{}, fmt.Errorf("get delivery project: %w", err)
	}
	items, err := service.repository.List(ctx)
	if err != nil {
		return ProjectSchedule{}, err
	}
	day := service.now().UTC()
	asOfDate := day.Format("2006-01-02")
	result := ProjectSchedule{ProjectID: projectID, AsOfDate: asOfDate}
	projectItems := make(map[string]WorkItem)
	capacityByOwner := make(map[string]*OwnerCapacity)
	for _, item := range items {
		if item.ProjectID != projectID || item.Kind == WorkItemKindEpic {
			continue
		}
		projectItems[item.ID] = item
		result.TotalItems++
		if item.StartDate != "" || item.DueDate != "" {
			result.ScheduledItems++
		} else {
			result.UnscheduledItems++
			result.Risks = append(result.Risks, scheduleRisk(item, "未排期"))
		}
		capacity := capacityByOwner[item.Owner]
		if capacity == nil {
			capacity = &OwnerCapacity{Owner: item.Owner}
			capacityByOwner[item.Owner] = capacity
		}
		weight := workItemWeight(item)
		progress := effectiveProgress(item)
		capacity.ItemCount++
		capacity.TotalEstimatePoints += weight
		capacity.CompletedEstimatePoints += weight * progress / 100
		capacity.RemainingEstimatePoints += weight * (100 - progress) / 100
		if item.Status == StatusBlocked {
			result.BlockedItems++
			capacity.BlockedItems++
			result.Risks = append(result.Risks, scheduleRisk(item, "事项受阻"))
		}
		if itemIsOverdue(item, day) {
			result.OverdueItems++
			capacity.OverdueItems++
			result.Risks = append(result.Risks, scheduleRisk(item, "已逾期"))
		}
	}
	dependencyBlocked := dependencyBlockedItemIDs(projectItems)
	for itemID := range dependencyBlocked {
		item, exists := projectItems[itemID]
		if !exists || effectiveProgress(item) >= 100 {
			continue
		}
		result.DependencyBlockedItems++
		result.Risks = append(result.Risks, scheduleRisk(item, "依赖未完成"))
	}
	result.Capacity = make([]OwnerCapacity, 0, len(capacityByOwner))
	for _, capacity := range capacityByOwner {
		result.Capacity = append(result.Capacity, *capacity)
	}
	sort.Slice(result.Capacity, func(left, right int) bool {
		return result.Capacity[left].Owner < result.Capacity[right].Owner
	})
	sort.Slice(result.Risks, func(left, right int) bool {
		if result.Risks[left].DueDate == result.Risks[right].DueDate {
			if result.Risks[left].ItemID == result.Risks[right].ItemID {
				return result.Risks[left].Reason < result.Risks[right].Reason
			}
			return result.Risks[left].ItemID < result.Risks[right].ItemID
		}
		if result.Risks[left].DueDate == "" {
			return false
		}
		if result.Risks[right].DueDate == "" {
			return true
		}
		return result.Risks[left].DueDate < result.Risks[right].DueDate
	})
	return result, nil
}

func workItemWeight(item WorkItem) float64 {
	if item.EstimatePoints > 0 {
		return item.EstimatePoints
	}
	return 1
}

func itemIsOverdue(item WorkItem, day time.Time) bool {
	dueDate, valid := itemDate(item.DueDate)
	if !valid || effectiveProgress(item) >= 100 {
		return false
	}
	return dueDate.Before(time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC))
}

func scheduleRisk(item WorkItem, reason string) ScheduleRisk {
	return ScheduleRisk{ItemID: item.ID, Title: item.Title, Owner: item.Owner, DueDate: item.DueDate, Reason: reason}
}

func dependencyBlockedItemIDs(items map[string]WorkItem) map[string]struct{} {
	blocked := make(map[string]struct{})
	for _, item := range items {
		for _, dependency := range item.Dependencies {
			target, exists := items[dependency.ItemID]
			if !exists {
				continue
			}
			switch dependency.Relation {
			case DependencyDependsOn:
				if effectiveProgress(target) < 100 {
					blocked[item.ID] = struct{}{}
				}
			case DependencyBlocks:
				if effectiveProgress(item) < 100 {
					blocked[target.ID] = struct{}{}
				}
			}
		}
	}
	return blocked
}

func (service *Service) FindSimilar(ctx context.Context, query SimilarityQuery) ([]SimilarityCandidate, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("delivery service is not configured")
	}
	query.Title = strings.TrimSpace(query.Title)
	if query.Title == "" {
		return nil, errors.New("delivery work item title is required")
	}
	query.ProjectID = strings.TrimSpace(query.ProjectID)
	if query.Kind == "" {
		query.Kind = WorkItemKindTask
	}
	if query.Limit <= 0 {
		query.Limit = 5
	}
	if query.Limit > 20 {
		query.Limit = 20
	}
	items, err := service.repository.List(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]SimilarityCandidate, 0, query.Limit)
	for _, item := range items {
		if item.Kind != query.Kind {
			continue
		}
		if query.ProjectID != "" {
			if item.ProjectID != query.ProjectID {
				continue
			}
		} else if item.ProjectID != "" || item.Board != query.Board {
			continue
		}
		score := workItemTitleSimilarity(query.Title, item.Title)
		if score < 0.55 {
			continue
		}
		candidates = append(candidates, SimilarityCandidate{
			WorkItem: item,
			Score:    score,
			Exact:    normalizedWorkItemTitle(query.Title) == normalizedWorkItemTitle(item.Title),
		})
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Score == candidates[right].Score {
			return candidates[left].ID < candidates[right].ID
		}
		return candidates[left].Score > candidates[right].Score
	})
	if len(candidates) > query.Limit {
		candidates = candidates[:query.Limit]
	}
	return candidates, nil
}

// UpdateWorkItem changes the editable planning, scheduling, scope, and trace
// fields while preserving the delivery gate state machine. Gate and close
// transitions remain explicit operations with their own evidence checks.
func (service *Service) UpdateWorkItem(ctx context.Context, id string, expectedRevision int64, input WorkItemUpdate) (WorkItem, error) {
	if service == nil || service.repository == nil {
		return WorkItem{}, errors.New("delivery service is not configured")
	}
	if expectedRevision <= 0 {
		return WorkItem{}, ErrInvalidExpectedRevision
	}
	item, err := service.repository.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return WorkItem{}, err
	}
	if item.Revision != expectedRevision {
		return WorkItem{}, ErrRevisionConflict
	}
	changed := false
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return WorkItem{}, errors.New("delivery item title is required")
		}
		item.Title = title
		changed = true
	}
	if input.Owner != nil {
		owner := strings.TrimSpace(*input.Owner)
		if owner == "" {
			return WorkItem{}, errors.New("delivery item owner is required")
		}
		item.Owner = owner
		changed = true
	}
	if input.Priority != nil {
		if strings.TrimSpace(string(*input.Priority)) == "" {
			return WorkItem{}, errors.New("delivery item priority is required")
		}
		item.Priority = *input.Priority
		changed = true
	}
	if input.ReleaseID != nil {
		item.ReleaseID = strings.TrimSpace(*input.ReleaseID)
		changed = true
	}
	if input.SprintID != nil {
		item.SprintID = strings.TrimSpace(*input.SprintID)
		changed = true
	}
	if input.MilestoneID != nil {
		item.MilestoneID = strings.TrimSpace(*input.MilestoneID)
		changed = true
	}
	if input.StartDate != nil {
		item.StartDate = strings.TrimSpace(*input.StartDate)
		changed = true
	}
	if input.DueDate != nil {
		item.DueDate = strings.TrimSpace(*input.DueDate)
		changed = true
	}
	if input.EstimatePoints != nil {
		if *input.EstimatePoints < 0 {
			return WorkItem{}, errors.New("delivery estimate points cannot be negative")
		}
		item.EstimatePoints = *input.EstimatePoints
		changed = true
	}
	if input.ProgressPercent != nil {
		if *input.ProgressPercent < 0 || *input.ProgressPercent > 100 {
			return WorkItem{}, errors.New("delivery progress percent must be between 0 and 100")
		}
		item.ProgressPercent = *input.ProgressPercent
		changed = true
	}
	if input.Dependencies != nil {
		dependencies, err := service.validateDependencies(ctx, item.ProjectID, item.ID, *input.Dependencies)
		if err != nil {
			return WorkItem{}, err
		}
		item.Dependencies = dependencies
		changed = true
	}
	if input.IoTBindings != nil {
		bindings, err := normalizeIoTBindings(*input.IoTBindings)
		if err != nil {
			return WorkItem{}, err
		}
		item.IoTBindings = bindings
		changed = true
	}
	now := service.now().UTC()
	if input.TraceLinks != nil {
		traceLinks, err := normalizeTraceLinks(*input.TraceLinks, now)
		if err != nil {
			return WorkItem{}, err
		}
		item.TraceLinks = traceLinks
		changed = true
	}
	if !changed {
		return WorkItem{}, ErrInvalidWorkItemUpdate
	}
	if err := service.validatePlanningLinks(ctx, item.ProjectID, item.ReleaseID, item.SprintID, item.MilestoneID); err != nil {
		return WorkItem{}, err
	}
	if err := service.rejectExactDuplicateExcept(ctx, CreateInput{
		Title:     item.Title,
		Board:     item.Board,
		ProjectID: item.ProjectID,
		Kind:      item.Kind,
	}, item.ID); err != nil {
		return WorkItem{}, err
	}
	item.Revision = expectedRevision + 1
	item.UpdatedAt = now
	item.Activities = append(item.Activities, service.newActivity(ctx, item, "updated", "更新交付事项", now))
	if err := service.persistMutation(ctx, "delivery.work-item.updated", item, func() error {
		return service.repository.Save(ctx, item, expectedRevision)
	}); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (service *Service) AddComment(ctx context.Context, id string, expectedRevision int64, input CommentInput) (Comment, error) {
	if service == nil || service.repository == nil {
		return Comment{}, errors.New("delivery service is not configured")
	}
	if expectedRevision <= 0 {
		return Comment{}, ErrInvalidExpectedRevision
	}
	item, err := service.repository.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return Comment{}, err
	}
	if item.Revision != expectedRevision {
		return Comment{}, ErrRevisionConflict
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return Comment{}, errors.New("delivery comment body is required")
	}
	now := service.now().UTC()
	comment := Comment{
		ID:        fmt.Sprintf("CMT-%s-%03d", item.ID, len(item.Comments)+1),
		Body:      body,
		Author:    actorFromContext(ctx),
		CreatedAt: now,
	}
	item.Comments = append(item.Comments, comment)
	item.Revision = expectedRevision + 1
	item.UpdatedAt = now
	item.Activities = append(item.Activities, service.newActivity(ctx, item, "commented", "新增事项评论", now))
	if err := service.persistMutation(ctx, "delivery.work-item.commented", item, func() error {
		return service.repository.Save(ctx, item, expectedRevision)
	}); err != nil {
		return Comment{}, err
	}
	comment.WorkItemRevision = item.Revision
	return comment, nil
}

func (service *Service) Search(ctx context.Context, filter WorkItemFilter) ([]WorkItem, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("delivery service is not configured")
	}
	items, err := service.repository.List(ctx)
	if err != nil {
		return nil, err
	}
	filter.ProjectID = strings.TrimSpace(filter.ProjectID)
	filter.Owner = strings.TrimSpace(filter.Owner)
	filter.ReleaseID = strings.TrimSpace(filter.ReleaseID)
	filter.SprintID = strings.TrimSpace(filter.SprintID)
	filter.MilestoneID = strings.TrimSpace(filter.MilestoneID)
	needle := normalizedWorkItemTitle(filter.Query)
	result := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if filter.ProjectID != "" && item.ProjectID != filter.ProjectID ||
			filter.Board != "" && item.Board != filter.Board ||
			filter.Owner != "" && item.Owner != filter.Owner ||
			filter.Status != "" && item.Status != filter.Status ||
			filter.Kind != "" && item.Kind != filter.Kind ||
			filter.ReleaseID != "" && item.ReleaseID != filter.ReleaseID ||
			filter.SprintID != "" && item.SprintID != filter.SprintID ||
			filter.MilestoneID != "" && item.MilestoneID != filter.MilestoneID {
			continue
		}
		if needle != "" && !workItemMatchesQuery(item, needle) {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func workItemMatchesQuery(item WorkItem, needle string) bool {
	values := []string{item.Title, item.Type, item.Owner, item.Plan, item.Solution, item.Blocker}
	for _, trace := range item.TraceLinks {
		values = append(values, trace.Reference, trace.Title, trace.Status)
	}
	for _, binding := range item.IoTBindings {
		values = append(values, binding.Reference, binding.Label)
	}
	for _, value := range values {
		if strings.Contains(normalizedWorkItemTitle(value), needle) {
			return true
		}
	}
	return false
}

func normalizeWorkItemFilter(filter WorkItemFilter) WorkItemFilter {
	filter.ProjectID = strings.TrimSpace(filter.ProjectID)
	filter.Owner = strings.TrimSpace(filter.Owner)
	filter.ReleaseID = strings.TrimSpace(filter.ReleaseID)
	filter.SprintID = strings.TrimSpace(filter.SprintID)
	filter.MilestoneID = strings.TrimSpace(filter.MilestoneID)
	filter.Query = strings.TrimSpace(filter.Query)
	return filter
}

func (service *Service) validatePlanningLinks(ctx context.Context, projectID, releaseID, sprintID, milestoneID string) error {
	projectID = strings.TrimSpace(projectID)
	releaseID = strings.TrimSpace(releaseID)
	sprintID = strings.TrimSpace(sprintID)
	milestoneID = strings.TrimSpace(milestoneID)
	if releaseID == "" && sprintID == "" && milestoneID == "" {
		return nil
	}
	if projectID == "" {
		return ErrPlanningScopeMismatch
	}
	if releaseID != "" {
		release, err := service.repository.GetRelease(ctx, releaseID)
		if err != nil {
			return fmt.Errorf("get delivery release: %w", err)
		}
		if release.ProjectID != projectID {
			return ErrPlanningScopeMismatch
		}
	}
	if sprintID != "" {
		sprint, err := service.repository.GetSprint(ctx, sprintID)
		if err != nil {
			return fmt.Errorf("get delivery sprint: %w", err)
		}
		if sprint.ProjectID != projectID {
			return ErrPlanningScopeMismatch
		}
	}
	if milestoneID != "" {
		milestone, err := service.repository.GetMilestone(ctx, milestoneID)
		if err != nil {
			return fmt.Errorf("get delivery milestone: %w", err)
		}
		if milestone.ProjectID != projectID {
			return ErrPlanningScopeMismatch
		}
	}
	return nil
}

func normalizeDate(value, label string, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return "", fmt.Errorf("%s is required", label)
		}
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return "", fmt.Errorf("%s must use YYYY-MM-DD", label)
	}
	return value, nil
}

func weekStartDate(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	var date time.Time
	var err error
	if value == "" {
		date = now.UTC()
	} else {
		date, err = time.Parse("2006-01-02", value)
		if err != nil {
			return time.Time{}, errors.New("delivery week start must use YYYY-MM-DD")
		}
	}
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	offset := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -offset), nil
}

func itemTouchesWeek(item WorkItem, start, end time.Time) bool {
	itemStart, hasStart := itemDate(item.StartDate)
	itemEnd, hasEnd := itemDate(item.DueDate)
	if !hasStart && !hasEnd {
		return true
	}
	if !hasStart {
		itemStart = itemEnd
	}
	if !hasEnd {
		itemEnd = itemStart
	}
	return !itemEnd.Before(start) && !itemStart.After(end)
}

func itemDate(value string) (time.Time, bool) {
	date, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	return date, err == nil
}

func earliestItemDate(item WorkItem) string {
	if _, ok := itemDate(item.StartDate); ok {
		return item.StartDate
	}
	if _, ok := itemDate(item.DueDate); ok {
		return item.DueDate
	}
	return ""
}

func effectiveProgress(item WorkItem) float64 {
	if item.Status == StatusReleased || item.Status == StatusClosed {
		return 100
	}
	if item.ProgressPercent < 0 {
		return 0
	}
	if item.ProgressPercent > 100 {
		return 100
	}
	return float64(item.ProgressPercent)
}

func (service *Service) AdvanceGate(ctx context.Context, id string, expectedRevision int64, next Gate, evidence []Evidence) (WorkItem, error) {
	if expectedRevision <= 0 {
		return WorkItem{}, ErrInvalidExpectedRevision
	}
	if len(evidence) == 0 {
		return WorkItem{}, ErrEvidenceRequired
	}
	item, err := service.repository.Get(ctx, id)
	if err != nil {
		return WorkItem{}, err
	}
	if item.Revision != expectedRevision {
		return WorkItem{}, ErrRevisionConflict
	}
	if !isNextGate(item.Gate, next) {
		return WorkItem{}, fmt.Errorf("%w: %s -> %s", ErrInvalidGateTransition, item.Gate, next)
	}
	var principal PrincipalSource
	if next == GateDevelopmentCompleted {
		var ok bool
		principal, ok = trustedPrincipalSource(ctx)
		if !ok {
			return WorkItem{}, ErrImplementationPrincipalRequired
		}
	}
	if next == GateProductionValidated {
		var ok bool
		principal, ok = namedJWTPrincipalSource(ctx)
		if !ok {
			return WorkItem{}, ErrProductionPrincipalRequired
		}
		if !validImplementationPrincipalSource(item.ImplementationPrincipal) {
			return WorkItem{}, ErrImplementationSourceRequired
		}
		if principal.TenantID != item.ImplementationPrincipal.TenantID {
			return WorkItem{}, ErrImplementationSourceRequired
		}
		if principal.sameSubject(item.ImplementationPrincipal) {
			return WorkItem{}, ErrImplementerCannotVerifyOwnChange
		}
	}
	now := service.now().UTC()
	for index := range evidence {
		evidence[index].Kind = strings.TrimSpace(evidence[index].Kind)
		evidence[index].Title = strings.TrimSpace(evidence[index].Title)
		evidence[index].Reference = strings.TrimSpace(evidence[index].Reference)
		if evidence[index].Kind == "" || evidence[index].Title == "" {
			return WorkItem{}, ErrEvidenceRequired
		}
		if evidence[index].RecordedAt.IsZero() {
			evidence[index].RecordedAt = now
		}
	}
	item.Gate = next
	item.Status = statusForGate(next)
	if next == GateDevelopmentCompleted {
		item.ImplementationPrincipal = principal
	}
	if next == GateProductionValidated {
		item.ProductionValidationPrincipal = principal
	}
	item.Evidence = append(item.Evidence, evidence...)
	item.Revision = expectedRevision + 1
	item.UpdatedAt = now
	item.Activities = append(item.Activities, service.newActivity(ctx, item, "gate_advanced", "推进交付关卡", now))
	if err := service.persistMutation(ctx, "delivery.work-item.gate-advanced", item, func() error {
		return service.repository.Save(ctx, item, expectedRevision)
	}); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (service *Service) Close(ctx context.Context, id string, expectedRevision int64, retrospective string) (WorkItem, error) {
	if expectedRevision <= 0 {
		return WorkItem{}, ErrInvalidExpectedRevision
	}
	item, err := service.repository.Get(ctx, id)
	if err != nil {
		return WorkItem{}, err
	}
	if item.Revision != expectedRevision {
		return WorkItem{}, ErrRevisionConflict
	}
	if item.Gate != GateProductionValidated {
		return WorkItem{}, ErrReleaseNotValidated
	}
	retrospective = strings.TrimSpace(retrospective)
	if retrospective == "" {
		return WorkItem{}, ErrRetrospectiveRequired
	}
	principal, ok := namedJWTPrincipalSource(ctx)
	if !ok {
		return WorkItem{}, ErrProductionPrincipalRequired
	}
	if !validImplementationPrincipalSource(item.ImplementationPrincipal) {
		return WorkItem{}, ErrImplementationSourceRequired
	}
	if principal.TenantID != item.ImplementationPrincipal.TenantID {
		return WorkItem{}, ErrImplementationSourceRequired
	}
	if principal.sameSubject(item.ImplementationPrincipal) {
		return WorkItem{}, ErrImplementerCannotVerifyOwnChange
	}
	item.Status = StatusClosed
	item.Retrospective = retrospective
	item.Revision = expectedRevision + 1
	now := service.now().UTC()
	item.UpdatedAt = now
	item.Activities = append(item.Activities, service.newActivity(ctx, item, "closed", "关闭交付事项", now))
	if err := service.persistMutation(ctx, "delivery.work-item.closed", item, func() error {
		return service.repository.Save(ctx, item, expectedRevision)
	}); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (service *Service) UpdateContext(ctx context.Context, id string, expectedRevision int64, input ContextUpdate) (WorkItem, error) {
	if expectedRevision <= 0 {
		return WorkItem{}, ErrInvalidExpectedRevision
	}
	item, err := service.repository.Get(ctx, id)
	if err != nil {
		return WorkItem{}, err
	}
	if item.Revision != expectedRevision {
		return WorkItem{}, ErrRevisionConflict
	}
	now := service.now().UTC()
	if input.Plan != nil {
		item.Plan = strings.TrimSpace(*input.Plan)
	}
	if input.Solution != nil {
		item.Solution = strings.TrimSpace(*input.Solution)
	}
	if input.Blocker != nil {
		item.Blocker = strings.TrimSpace(*input.Blocker)
		if item.Blocker != "" {
			if item.Status == StatusClosed || item.Status == StatusReleased {
				return WorkItem{}, errors.New("released or closed delivery item cannot be blocked")
			}
			item.Status = StatusBlocked
		} else if item.Status == StatusBlocked {
			item.Status = statusForGate(item.Gate)
		}
	}
	if input.Decision != nil {
		decision := *input.Decision
		decision.Title = strings.TrimSpace(decision.Title)
		decision.Context = strings.TrimSpace(decision.Context)
		decision.Outcome = strings.TrimSpace(decision.Outcome)
		decision.Consequences = strings.TrimSpace(decision.Consequences)
		if decision.Title == "" || decision.Outcome == "" {
			return WorkItem{}, errors.New("decision title and outcome are required")
		}
		if strings.TrimSpace(decision.ID) == "" {
			decision.ID = fmt.Sprintf("ADR-%s-%03d", item.ID, len(item.Decisions)+1)
		}
		if decision.CreatedAt.IsZero() {
			decision.CreatedAt = now
		}
		item.Decisions = append(item.Decisions, decision)
	}
	item.Revision = expectedRevision + 1
	item.UpdatedAt = now
	item.Activities = append(item.Activities, service.newActivity(ctx, item, "context_updated", "更新事项上下文", now))
	if err := service.persistMutation(ctx, "delivery.work-item.context-updated", item, func() error {
		return service.repository.Save(ctx, item, expectedRevision)
	}); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (service *Service) List(ctx context.Context) ([]WorkItem, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("delivery service is not configured")
	}
	return service.repository.List(ctx)
}

func (service *Service) newActivity(ctx context.Context, item WorkItem, activityType, summary string, occurredAt time.Time) Activity {
	return Activity{
		ID:         fmt.Sprintf("ACT-%s-%03d", item.ID, len(item.Activities)+1),
		Type:       strings.TrimSpace(activityType),
		Summary:    strings.TrimSpace(summary),
		Actor:      actorFromContext(ctx),
		OccurredAt: occurredAt.UTC(),
	}
}

func actorFromContext(ctx context.Context) string {
	principal, ok := identity.FromContext(ctx)
	if !ok {
		return "system"
	}
	if value := strings.TrimSpace(principal.UserID); value != "" {
		return value
	}
	if value := strings.TrimSpace(principal.Subject); value != "" {
		return value
	}
	return "system"
}

func trustedPrincipalSource(ctx context.Context) (PrincipalSource, bool) {
	principal, ok := identity.FromContext(ctx)
	if !ok || !principal.Authenticated || !canonicalPrincipalValue(principal.TenantID) {
		return PrincipalSource{}, false
	}
	switch principal.AuthMethod {
	case identity.AuthMethodJWT:
		if !canonicalPrincipalValue(principal.UserID) {
			return PrincipalSource{}, false
		}
		return PrincipalSource{Kind: "human", AuthMethod: identity.AuthMethodJWT, SubjectID: principal.UserID, TenantID: principal.TenantID}, true
	case identity.AuthMethodServiceToken:
		serviceAccountID, ok := strings.CutPrefix(principal.Subject, "service-account/")
		if principal.UserID != "" || !canonicalPrincipalValue(principal.Subject) || !canonicalPrincipalValue(serviceAccountID) || !ok {
			return PrincipalSource{}, false
		}
		return PrincipalSource{Kind: "service", AuthMethod: identity.AuthMethodServiceToken, SubjectID: principal.Subject, TenantID: principal.TenantID}, true
	case identity.AuthMethodAPIKey:
		if !canonicalPrincipalValue(principal.UserID) {
			return PrincipalSource{}, false
		}
		return PrincipalSource{Kind: "development-api-key", AuthMethod: identity.AuthMethodAPIKey, SubjectID: principal.UserID, TenantID: principal.TenantID}, true
	default:
		return PrincipalSource{}, false
	}
}

func namedJWTPrincipalSource(ctx context.Context) (PrincipalSource, bool) {
	principal, ok := identity.FromContext(ctx)
	if !ok || principal.AuthMethod != identity.AuthMethodJWT || !canonicalPrincipalValue(principal.UserID) {
		return PrincipalSource{}, false
	}
	source, ok := trustedPrincipalSource(ctx)
	return source, ok && source.Kind == "human"
}

func canonicalPrincipalValue(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func (source PrincipalSource) sameSubject(other PrincipalSource) bool {
	return source.SubjectID == other.SubjectID
}

func validImplementationPrincipalSource(source PrincipalSource) bool {
	if !canonicalPrincipalValue(source.TenantID) || !canonicalPrincipalValue(source.SubjectID) {
		return false
	}
	switch source.Kind {
	case "human":
		return source.AuthMethod == identity.AuthMethodJWT
	case "service":
		serviceAccountID, ok := strings.CutPrefix(source.SubjectID, "service-account/")
		return source.AuthMethod == identity.AuthMethodServiceToken && ok && canonicalPrincipalValue(serviceAccountID)
	case "development-api-key":
		return source.AuthMethod == identity.AuthMethodAPIKey
	default:
		return false
	}
}

func normalizeIoTBindings(values []IoTBinding) ([]IoTBinding, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]IoTBinding, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value.Kind = IoTBindingKind(strings.TrimSpace(string(value.Kind)))
		value.Reference = strings.TrimSpace(value.Reference)
		value.Label = strings.TrimSpace(value.Label)
		if !isIoTBindingKind(value.Kind) || value.Reference == "" {
			return nil, ErrInvalidIoTBinding
		}
		key := string(value.Kind) + "\x00" + value.Reference
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		value.Attributes = cloneStringMap(value.Attributes)
		result = append(result, value)
	}
	return result, nil
}

func isIoTBindingKind(value IoTBindingKind) bool {
	switch value {
	case IoTBindingDevice, IoTBindingFirmware, IoTBindingCustomer, IoTBindingEnvironment, IoTBindingRolloutBatch:
		return true
	default:
		return false
	}
}

func normalizeTraceLinks(values []TraceLink, now time.Time) ([]TraceLink, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]TraceLink, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value.Kind = TraceKind(strings.TrimSpace(string(value.Kind)))
		value.Reference = strings.TrimSpace(value.Reference)
		value.Title = strings.TrimSpace(value.Title)
		value.URL = strings.TrimSpace(value.URL)
		value.Status = strings.TrimSpace(value.Status)
		if !isTraceKind(value.Kind) || value.Reference == "" {
			return nil, ErrInvalidTraceLink
		}
		key := string(value.Kind) + "\x00" + value.Reference
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if value.RecordedAt.IsZero() {
			value.RecordedAt = now.UTC()
		} else {
			value.RecordedAt = value.RecordedAt.UTC()
		}
		result = append(result, value)
	}
	return result, nil
}

func isTraceKind(value TraceKind) bool {
	switch value {
	case TracePullRequest, TraceBuild, TraceTest, TraceDefect, TraceRelease:
		return true
	default:
		return false
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func (service *Service) Dashboard(ctx context.Context) ([]WorkItem, error) {
	return service.List(ctx)
}

func (service *Service) Sync(ctx context.Context) error {
	if service == nil || service.repository == nil {
		return errors.New("delivery service is not configured")
	}
	return service.export(ctx)
}

func (service *Service) persistMutation(ctx context.Context, eventType string, item WorkItem, save func() error) error {
	if save == nil {
		return errors.New("delivery mutation persistence is not configured")
	}
	if service.stager != nil {
		if err := service.stager.Stage(ctx, eventType, item); err != nil {
			return err
		}
		return save()
	}
	if err := save(); err != nil {
		return err
	}
	return service.export(ctx)
}

func (service *Service) persistProjectMutation(ctx context.Context, eventType string, project Project, save func() error) error {
	if save == nil {
		return errors.New("delivery project persistence is not configured")
	}
	if service.stager != nil {
		if err := service.stager.Stage(ctx, eventType, WorkItem{ID: project.ID, UpdatedAt: project.UpdatedAt}); err != nil {
			return err
		}
		return save()
	}
	if err := save(); err != nil {
		return err
	}
	return service.export(ctx)
}

func (service *Service) export(ctx context.Context) error {
	if service.exporter == nil {
		return nil
	}
	items, err := service.repository.List(ctx)
	if err != nil {
		return err
	}
	return service.exporter.Export(ctx, items)
}

func (service *Service) nextID(ctx context.Context, now time.Time) (string, error) {
	for attempt := 0; attempt < 10_000; attempt++ {
		candidate := fmt.Sprintf("IOT-%s-%04d", now.Format("20060102"), service.sequence.Add(1))
		_, err := service.repository.Get(ctx, candidate)
		switch {
		case errors.Is(err, ErrNotFound):
			return candidate, nil
		case err != nil:
			return "", fmt.Errorf("check generated delivery item ID: %w", err)
		}
	}
	return "", errors.New("could not allocate a delivery item ID")
}

func (service *Service) nextProjectID(ctx context.Context, now time.Time) (string, error) {
	for attempt := 0; attempt < 10_000; attempt++ {
		candidate := fmt.Sprintf("PRJ-%s-%04d", now.Format("20060102"), service.sequence.Add(1))
		_, err := service.repository.GetProject(ctx, candidate)
		switch {
		case errors.Is(err, ErrNotFound):
			return candidate, nil
		case err != nil:
			return "", fmt.Errorf("check generated delivery project ID: %w", err)
		}
	}
	return "", errors.New("could not allocate a delivery project ID")
}

func (service *Service) nextReleaseID(ctx context.Context, now time.Time) (string, error) {
	return service.nextPlanningID(ctx, "REL", now, func(id string) error {
		_, err := service.repository.GetRelease(ctx, id)
		return err
	})
}

func (service *Service) nextSprintID(ctx context.Context, now time.Time) (string, error) {
	return service.nextPlanningID(ctx, "SPR", now, func(id string) error {
		_, err := service.repository.GetSprint(ctx, id)
		return err
	})
}

func (service *Service) nextMilestoneID(ctx context.Context, now time.Time) (string, error) {
	return service.nextPlanningID(ctx, "MS", now, func(id string) error {
		_, err := service.repository.GetMilestone(ctx, id)
		return err
	})
}

func (service *Service) nextSavedViewID(ctx context.Context, now time.Time) (string, error) {
	return service.nextPlanningID(ctx, "VIEW", now, func(id string) error {
		views, err := service.repository.ListSavedViews(ctx, "")
		if err != nil {
			return err
		}
		for _, view := range views {
			if view.ID == id {
				return nil
			}
		}
		return ErrNotFound
	})
}

func (service *Service) nextPlanningID(ctx context.Context, prefix string, now time.Time, find func(string) error) (string, error) {
	for attempt := 0; attempt < 10_000; attempt++ {
		candidate := fmt.Sprintf("%s-%s-%04d", prefix, now.Format("20060102"), service.sequence.Add(1))
		err := find(candidate)
		switch {
		case errors.Is(err, ErrNotFound):
			return candidate, nil
		case err != nil:
			return "", fmt.Errorf("check generated delivery %s ID: %w", strings.ToLower(prefix), err)
		}
	}
	return "", fmt.Errorf("could not allocate a delivery %s ID", strings.ToLower(prefix))
}

func isNextGate(current, next Gate) bool {
	switch current {
	case GatePlanning:
		return next == GateSolutionReviewed
	case GateSolutionReviewed:
		return next == GateDevelopmentCompleted
	case GateDevelopmentCompleted:
		return next == GateTestPassed
	case GateTestPassed:
		return next == GateProductionValidated
	default:
		return false
	}
}

func statusForGate(gate Gate) Status {
	switch gate {
	case GatePlanning:
		return StatusPlanned
	case GateTestPassed:
		return StatusVerifying
	case GateProductionValidated:
		return StatusReleased
	default:
		return StatusInProgress
	}
}
