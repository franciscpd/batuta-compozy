package routing

import "fmt"

func validateMaterializedGraph(graph ValidatedTaskGraph) error {
	tasks := make(map[string]ValidatedTask, len(graph.Tasks))
	for _, task := range graph.Tasks {
		if _, duplicate := tasks[task.ID]; duplicate {
			return fmt.Errorf("%w: duplicate task in validated graph", ErrReauthoringRequired)
		}
		tasks[task.ID] = task
	}
	for _, task := range graph.Tasks {
		for _, dependency := range task.Dependencies {
			if dependency == task.ID {
				return fmt.Errorf("%w: task depends on itself", ErrReauthoringRequired)
			}
			if _, exists := tasks[dependency]; !exists {
				return fmt.Errorf("%w: dependency references a missing task", ErrReauthoringRequired)
			}
		}
	}

	const (
		unvisited = iota
		visiting
		visited
	)
	states := make(map[string]int, len(tasks))
	var visit func(string) error
	visit = func(id string) error {
		switch states[id] {
		case visiting:
			return fmt.Errorf("%w: task dependency cycle", ErrReauthoringRequired)
		case visited:
			return nil
		}
		states[id] = visiting
		for _, dependency := range tasks[id].Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		states[id] = visited
		return nil
	}
	for _, task := range graph.Tasks {
		if err := visit(task.ID); err != nil {
			return err
		}
	}
	return nil
}
