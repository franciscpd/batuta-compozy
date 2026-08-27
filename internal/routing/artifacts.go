package routing

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	maxTaskArtifactBytes = 1 << 20
	maxTaskSetBytes      = 16 << 20
)

var (
	ErrInvalidSlug                = errors.New("routing: invalid task-set slug")
	ErrArtifactOutsideTrustedRoot = errors.New("routing: artifact outside trusted root")
	ErrReauthoringRequired        = errors.New("routing: task metadata requires reauthoring")

	canonicalSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	taskFilename  = regexp.MustCompile(`^task_[0-9]+\.md$`)
)

type Domain string

const (
	DomainBackend   Domain = "backend"
	DomainFrontend  Domain = "frontend"
	DomainMobile    Domain = "mobile"
	DomainData      Domain = "data"
	DomainInfra     Domain = "infra"
	DomainSecurity  Domain = "security"
	DomainTesting   Domain = "testing"
	DomainDocs      Domain = "docs"
	DomainGeneral   Domain = "general"
	DomainFullstack Domain = "fullstack"
)

type Complexity string

const (
	ComplexityLow      Complexity = "low"
	ComplexityMedium   Complexity = "medium"
	ComplexityHigh     Complexity = "high"
	ComplexityCritical Complexity = "critical"
)

type TaskArtifact struct {
	ID           string
	Title        string
	Status       string
	Domain       Domain
	Complexity   Complexity
	Dependencies []string
	Content      []byte
	Digest       string
}

type TaskSet struct {
	Slug   string
	Tasks  []TaskArtifact
	Digest string
}

type DeliveryTaskSnapshotEntry struct {
	ID           string     `json:"task_id"`
	Digest       string     `json:"digest"`
	Dependencies []string   `json:"dependencies"`
	Domain       Domain     `json:"domain"`
	Complexity   Complexity `json:"complexity"`
	Status       string     `json:"status"`
}

type DeliveryTaskSnapshot struct {
	Digest            string                      `json:"digest"`
	Tasks             []DeliveryTaskSnapshotEntry `json:"tasks"`
	IncompleteTaskIDs []string                    `json:"incomplete_task_ids"`
	ItemTaskIDs       map[int]string              `json:"item_task_ids"`
}

type DeliveryTaskProgress struct {
	IncompleteTaskIDs []string       `json:"incomplete_task_ids"`
	ItemTaskIDs       map[int]string `json:"item_task_ids"`
}

func (initial DeliveryTaskSnapshot) Reconcile(current DeliveryTaskSnapshot) (DeliveryTaskProgress, error) {
	if err := validateStandaloneTaskSnapshot(initial); err != nil {
		return DeliveryTaskProgress{}, err
	}
	if err := validateStandaloneTaskSnapshot(current); err != nil {
		return DeliveryTaskProgress{}, err
	}
	if len(initial.Tasks) != len(current.Tasks) || len(initial.ItemTaskIDs) != len(current.ItemTaskIDs) {
		return DeliveryTaskProgress{}, ErrReauthoringRequired
	}
	for index := range initial.Tasks {
		before, after := initial.Tasks[index], current.Tasks[index]
		if before.ID != after.ID || before.Domain != after.Domain || before.Complexity != after.Complexity ||
			!slices.Equal(before.Dependencies, after.Dependencies) || initial.ItemTaskIDs[index] != before.ID || current.ItemTaskIDs[index] != before.ID {
			return DeliveryTaskProgress{}, ErrReauthoringRequired
		}
		switch before.Status {
		case "pending":
			if after.Status != "pending" && after.Status != "completed" {
				return DeliveryTaskProgress{}, ErrReauthoringRequired
			}
			if after.Status == "pending" && before.Digest != after.Digest {
				return DeliveryTaskProgress{}, ErrReauthoringRequired
			}
		case "completed":
			if after.Status != "completed" {
				return DeliveryTaskProgress{}, ErrReauthoringRequired
			}
		default:
			return DeliveryTaskProgress{}, ErrReauthoringRequired
		}
	}
	return DeliveryTaskProgress{
		IncompleteTaskIDs: append([]string(nil), current.IncompleteTaskIDs...),
		ItemTaskIDs:       cloneItemTaskIDs(initial.ItemTaskIDs),
	}, nil
}

func validateStandaloneTaskSnapshot(snapshot DeliveryTaskSnapshot) error {
	set := TaskSet{Slug: "snapshot", Tasks: make([]TaskArtifact, 0, len(snapshot.Tasks))}
	for _, task := range snapshot.Tasks {
		set.Tasks = append(set.Tasks, TaskArtifact{
			ID: task.ID, Status: task.Status, Domain: task.Domain, Complexity: task.Complexity,
			Dependencies: append([]string(nil), task.Dependencies...), Digest: task.Digest,
		})
	}
	recomputed, err := set.DeliverySnapshot()
	if err != nil || recomputed.Digest != snapshot.Digest || !slices.Equal(recomputed.IncompleteTaskIDs, snapshot.IncompleteTaskIDs) ||
		!mapsEqualItemTaskIDs(recomputed.ItemTaskIDs, snapshot.ItemTaskIDs) {
		return ErrReauthoringRequired
	}
	return nil
}

func cloneItemTaskIDs(values map[int]string) map[int]string {
	cloned := make(map[int]string, len(values))
	for index, taskID := range values {
		cloned[index] = taskID
	}
	return cloned
}

func mapsEqualItemTaskIDs(left, right map[int]string) bool {
	if len(left) != len(right) {
		return false
	}
	for index, taskID := range left {
		if right[index] != taskID {
			return false
		}
	}
	return true
}

type ArtifactLoader struct {
	root string
}

func NewArtifactLoader(workspaceRoot string) (*ArtifactLoader, error) {
	if strings.TrimSpace(workspaceRoot) == "" || !filepath.IsAbs(workspaceRoot) {
		return nil, errors.New("routing: trusted workspace root must be absolute")
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(workspaceRoot))
	if err != nil {
		return nil, errors.New("routing: trusted workspace root is unavailable")
	}
	return &ArtifactLoader{root: root}, nil
}

func (l *ArtifactLoader) Load(slug string) (TaskSet, error) {
	if !canonicalSlug.MatchString(slug) {
		return TaskSet{}, ErrInvalidSlug
	}
	directory, err := l.resolveContained(filepath.Join(".compozy", "tasks", slug))
	if err != nil {
		return TaskSet{}, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return TaskSet{}, errors.New("routing: task-set directory is unavailable")
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if taskFilename.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		return TaskSet{}, fmt.Errorf("%w: task set has no authored tasks", ErrReauthoringRequired)
	}
	manifest, err := l.loadTaskManifest(slug, names)
	if err != nil {
		return TaskSet{}, err
	}

	tasks := make([]TaskArtifact, 0, len(names))
	totalBytes := int64(len(manifest.payload))
	for _, name := range manifest.orderedFiles {
		path, err := l.resolveContained(filepath.Join(".compozy", "tasks", slug, name))
		if err != nil {
			return TaskSet{}, err
		}
		payload, err := readBoundedFile(path, maxTaskArtifactBytes)
		if err != nil {
			return TaskSet{}, err
		}
		totalBytes += int64(len(payload))
		if totalBytes > maxTaskSetBytes {
			return TaskSet{}, errors.New("routing: task-set byte budget exceeded")
		}
		task, err := parseTaskArtifact(strings.TrimSuffix(name, ".md"), payload)
		if err != nil {
			return TaskSet{}, fmt.Errorf("routing: invalid %s: %w", name, err)
		}
		task.Dependencies = append([]string(nil), manifest.dependencies[task.ID]...)
		tasks = append(tasks, task)
	}

	set := TaskSet{Slug: slug, Tasks: tasks}
	snapshot, err := set.DeliverySnapshot()
	if err != nil {
		return TaskSet{}, err
	}
	set.Digest = snapshot.Digest
	return set, nil
}

func (set TaskSet) DeliverySnapshot() (DeliveryTaskSnapshot, error) {
	if !canonicalSlug.MatchString(set.Slug) || len(set.Tasks) == 0 {
		return DeliveryTaskSnapshot{}, ErrReauthoringRequired
	}
	snapshot := DeliveryTaskSnapshot{
		Tasks:             make([]DeliveryTaskSnapshotEntry, 0, len(set.Tasks)),
		IncompleteTaskIDs: make([]string, 0, len(set.Tasks)), ItemTaskIDs: make(map[int]string, len(set.Tasks)),
	}
	seen := make(map[string]struct{}, len(set.Tasks))
	for index, task := range set.Tasks {
		if _, duplicate := seen[task.ID]; duplicate || !taskIDPattern.MatchString(task.ID) || !canonicalHexHash.MatchString(task.Digest) ||
			!task.Domain.Valid() || !task.Complexity.Valid() || (task.Status != "pending" && task.Status != "completed") {
			return DeliveryTaskSnapshot{}, ErrReauthoringRequired
		}
		seen[task.ID] = struct{}{}
		entry := DeliveryTaskSnapshotEntry{
			ID: task.ID, Digest: task.Digest, Dependencies: append([]string(nil), task.Dependencies...),
			Domain: task.Domain, Complexity: task.Complexity, Status: task.Status,
		}
		for _, dependency := range entry.Dependencies {
			if _, exists := seen[dependency]; !exists {
				return DeliveryTaskSnapshot{}, ErrReauthoringRequired
			}
		}
		snapshot.Tasks = append(snapshot.Tasks, entry)
		snapshot.ItemTaskIDs[index] = task.ID
		if task.Status != "completed" {
			snapshot.IncompleteTaskIDs = append(snapshot.IncompleteTaskIDs, task.ID)
		}
	}
	payload, err := json.Marshal(snapshot.Tasks)
	if err != nil {
		return DeliveryTaskSnapshot{}, errors.New("routing: encode task snapshot failed")
	}
	digest := sha256.Sum256(payload)
	snapshot.Digest = hex.EncodeToString(digest[:])
	return snapshot, nil
}

func (l *ArtifactLoader) resolveContained(relative string) (string, error) {
	candidate := filepath.Join(l.root, relative)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", errors.New("routing: artifact is unavailable")
	}
	contained, err := filepath.Rel(l.root, resolved)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) || filepath.IsAbs(contained) {
		return "", ErrArtifactOutsideTrustedRoot
	}
	return resolved, nil
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("routing: artifact is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("routing: artifact is unavailable")
	}
	payload, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, errors.New("routing: artifact is unavailable")
	}
	if int64(len(payload)) > limit {
		return nil, errors.New("routing: task artifact byte budget exceeded")
	}
	return payload, nil
}

type taskFrontmatter struct {
	status       string
	title        string
	domain       string
	complexity   string
	dependencies []string
}

func parseTaskArtifact(id string, payload []byte) (TaskArtifact, error) {
	frontmatter, err := parseTaskFrontmatter(payload)
	if err != nil {
		return TaskArtifact{}, err
	}
	domain := Domain(frontmatter.domain)
	complexity := Complexity(frontmatter.complexity)
	if !domain.Valid() || !complexity.Valid() || (frontmatter.status != "pending" && frontmatter.status != "completed") || frontmatter.title == "" {
		return TaskArtifact{}, ErrReauthoringRequired
	}
	for _, dependency := range frontmatter.dependencies {
		if !strings.HasPrefix(dependency, "task_") || !taskFilename.MatchString(dependency+".md") || dependency == id {
			return TaskArtifact{}, ErrReauthoringRequired
		}
	}
	digest := sha256.Sum256(payload)
	return TaskArtifact{
		ID:           id,
		Title:        frontmatter.title,
		Status:       frontmatter.status,
		Domain:       domain,
		Complexity:   complexity,
		Dependencies: append([]string(nil), frontmatter.dependencies...),
		Content:      append([]byte(nil), payload...),
		Digest:       hex.EncodeToString(digest[:]),
	}, nil
}

func parseTaskFrontmatter(payload []byte) (taskFrontmatter, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(payload)))
	scanner.Buffer(make([]byte, 1024), maxTaskArtifactBytes)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return taskFrontmatter{}, ErrReauthoringRequired
	}
	values := make(map[string]string)
	closed := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			closed = true
			break
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return taskFrontmatter{}, ErrReauthoringRequired
		}
		switch key {
		case "status", "title", "type", "complexity":
		default:
			return taskFrontmatter{}, ErrReauthoringRequired
		}
		if _, duplicate := values[key]; duplicate {
			return taskFrontmatter{}, ErrReauthoringRequired
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil || !closed {
		return taskFrontmatter{}, ErrReauthoringRequired
	}
	dependencies, err := parseInlineDependencies(values["dependencies"])
	if err != nil {
		return taskFrontmatter{}, err
	}
	return taskFrontmatter{
		status:       unquoteScalar(values["status"]),
		title:        unquoteScalar(values["title"]),
		domain:       unquoteScalar(values["type"]),
		complexity:   unquoteScalar(values["complexity"]),
		dependencies: dependencies,
	}, nil
}

func parseInlineDependencies(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil, ErrReauthoringRequired
	}
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return nil, nil
	}
	parts := strings.Split(inner, ",")
	dependencies := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		dependency := unquoteScalar(strings.TrimSpace(part))
		if dependency == "" {
			return nil, ErrReauthoringRequired
		}
		if _, duplicate := seen[dependency]; duplicate {
			return nil, ErrReauthoringRequired
		}
		seen[dependency] = struct{}{}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, nil
}

func unquoteScalar(value string) string {
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		return strings.TrimSpace(value[1 : len(value)-1])
	}
	return strings.TrimSpace(value)
}

func (d Domain) Valid() bool {
	switch d {
	case DomainBackend, DomainFrontend, DomainMobile, DomainData, DomainInfra, DomainSecurity, DomainTesting, DomainDocs, DomainGeneral, DomainFullstack:
		return true
	default:
		return false
	}
}

func (c Complexity) Valid() bool {
	switch c {
	case ComplexityLow, ComplexityMedium, ComplexityHigh, ComplexityCritical:
		return true
	default:
		return false
	}
}

func writeDigestPart(writer io.Writer, value string) {
	_, _ = io.WriteString(writer, fmt.Sprintf("%d:", len(value)))
	_, _ = io.WriteString(writer, value)
}
