package routing

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const taskManifestSchemaVersion = "compozy.tasks/v2"

var taskIDPattern = regexp.MustCompile(`^task_([0-9]+)$`)

type taskManifest struct {
	SchemaVersion string            `yaml:"schema_version"`
	Workflow      string            `yaml:"workflow"`
	Graph         taskManifestGraph `yaml:"graph"`
}

type taskManifestGraph struct {
	Nodes []taskManifestNode `yaml:"nodes"`
	Edges []taskManifestEdge `yaml:"edges"`
}

type taskManifestNode struct {
	ID   string `yaml:"id"`
	File string `yaml:"file"`
}

type taskManifestEdge struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

type loadedTaskManifest struct {
	payload      []byte
	orderedFiles []string
	dependencies map[string][]string
}

func (l *ArtifactLoader) loadTaskManifest(slug string, taskFiles []string) (loadedTaskManifest, error) {
	path, err := l.resolveContained(filepath.Join(".compozy", "tasks", slug, "_tasks.md"))
	if err != nil {
		return loadedTaskManifest{}, fmt.Errorf("%w: canonical _tasks.md is unavailable", ErrReauthoringRequired)
	}
	payload, err := readBoundedFile(path, maxTaskArtifactBytes)
	if err != nil {
		return loadedTaskManifest{}, err
	}
	metadata, err := taskManifestFrontmatter(payload)
	if err != nil {
		return loadedTaskManifest{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(metadata))
	decoder.KnownFields(true)
	var manifest taskManifest
	if err := decoder.Decode(&manifest); err != nil {
		return loadedTaskManifest{}, fmt.Errorf("%w: invalid _tasks.md manifest", ErrReauthoringRequired)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return loadedTaskManifest{}, fmt.Errorf("%w: _tasks.md must contain one YAML document", ErrReauthoringRequired)
	}
	manifest.SchemaVersion = strings.TrimSpace(manifest.SchemaVersion)
	manifest.Workflow = strings.TrimSpace(manifest.Workflow)
	if manifest.SchemaVersion != taskManifestSchemaVersion || manifest.Workflow != slug || len(manifest.Graph.Nodes) == 0 {
		return loadedTaskManifest{}, fmt.Errorf("%w: _tasks.md identity is invalid", ErrReauthoringRequired)
	}
	return validateAndOrderTaskManifest(payload, manifest, taskFiles)
}

func taskManifestFrontmatter(payload []byte) ([]byte, error) {
	lines := bytes.Split(payload, []byte("\n"))
	if len(lines) < 3 || strings.TrimSpace(string(lines[0])) != "---" {
		return nil, fmt.Errorf("%w: _tasks.md has no YAML frontmatter", ErrReauthoringRequired)
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(string(lines[index])) == "---" {
			return bytes.Join(lines[1:index], []byte("\n")), nil
		}
	}
	return nil, fmt.Errorf("%w: _tasks.md frontmatter is unterminated", ErrReauthoringRequired)
}

func validateAndOrderTaskManifest(payload []byte, manifest taskManifest, taskFiles []string) (loadedTaskManifest, error) {
	fileSet := make(map[string]struct{}, len(taskFiles))
	for _, name := range taskFiles {
		fileSet[name] = struct{}{}
	}
	filesByID := make(map[string]string, len(manifest.Graph.Nodes))
	numbers := make(map[string]int, len(manifest.Graph.Nodes))
	for index := range manifest.Graph.Nodes {
		node := &manifest.Graph.Nodes[index]
		node.ID = strings.TrimSpace(node.ID)
		node.File = filepath.ToSlash(strings.TrimSpace(node.File))
		matches := taskIDPattern.FindStringSubmatch(node.ID)
		number := 0
		if len(matches) == 2 {
			number, _ = strconv.Atoi(matches[1])
		}
		if number <= 0 || node.File != node.ID+".md" || filepath.Base(node.File) != node.File {
			return loadedTaskManifest{}, fmt.Errorf("%w: _tasks.md node identity is invalid", ErrReauthoringRequired)
		}
		if _, exists := filesByID[node.ID]; exists {
			return loadedTaskManifest{}, fmt.Errorf("%w: duplicate _tasks.md node", ErrReauthoringRequired)
		}
		if _, exists := fileSet[node.File]; !exists {
			return loadedTaskManifest{}, fmt.Errorf("%w: _tasks.md node file is absent", ErrReauthoringRequired)
		}
		filesByID[node.ID] = node.File
		numbers[node.ID] = number
	}
	if len(filesByID) != len(fileSet) {
		return loadedTaskManifest{}, fmt.Errorf("%w: _tasks.md does not cover every task file", ErrReauthoringRequired)
	}
	predecessors := make(map[string]int, len(filesByID))
	successors := make(map[string][]string, len(filesByID))
	dependencies := make(map[string][]string, len(filesByID))
	for id := range filesByID {
		predecessors[id] = 0
	}
	seenEdges := make(map[string]struct{}, len(manifest.Graph.Edges))
	for index := range manifest.Graph.Edges {
		edge := &manifest.Graph.Edges[index]
		edge.From = strings.TrimSpace(edge.From)
		edge.To = strings.TrimSpace(edge.To)
		if edge.From == edge.To || filesByID[edge.From] == "" || filesByID[edge.To] == "" {
			return loadedTaskManifest{}, fmt.Errorf("%w: _tasks.md edge is invalid", ErrReauthoringRequired)
		}
		key := edge.From + "\x00" + edge.To
		if _, duplicate := seenEdges[key]; duplicate {
			return loadedTaskManifest{}, fmt.Errorf("%w: duplicate _tasks.md edge", ErrReauthoringRequired)
		}
		seenEdges[key] = struct{}{}
		successors[edge.From] = append(successors[edge.From], edge.To)
		dependencies[edge.To] = append(dependencies[edge.To], edge.From)
		predecessors[edge.To]++
	}
	for id := range dependencies {
		slices.Sort(dependencies[id])
	}
	ready := make([]string, 0, len(filesByID))
	for id, count := range predecessors {
		if count == 0 {
			ready = append(ready, id)
		}
	}
	sortTaskManifestIDs(ready, numbers)
	ordered := make([]string, 0, len(filesByID))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, filesByID[id])
		sortTaskManifestIDs(successors[id], numbers)
		for _, successor := range successors[id] {
			predecessors[successor]--
			if predecessors[successor] == 0 {
				ready = append(ready, successor)
			}
		}
		sortTaskManifestIDs(ready, numbers)
	}
	if len(ordered) != len(filesByID) {
		return loadedTaskManifest{}, fmt.Errorf("%w: _tasks.md graph contains a cycle", ErrReauthoringRequired)
	}
	return loadedTaskManifest{payload: append([]byte(nil), payload...), orderedFiles: ordered, dependencies: dependencies}, nil
}

func sortTaskManifestIDs(ids []string, numbers map[string]int) {
	slices.SortStableFunc(ids, func(left, right string) int {
		if numbers[left] != numbers[right] {
			return numbers[left] - numbers[right]
		}
		return strings.Compare(left, right)
	})
}
