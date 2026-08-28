---
schema_version: "compozy.tasks/v2"
workflow: parallel-demo
graph:
  nodes:
    - id: task_01
      file: task_01.md
    - id: task_02
      file: task_02.md
    - id: task_03
      file: task_03.md
    - id: task_04
      file: task_04.md
    - id: task_05
      file: task_05.md
  edges:
    - from: task_01
      to: task_05
    - from: task_02
      to: task_05
    - from: task_03
      to: task_05
    - from: task_04
      to: task_05
---

# Parallel delivery deterministic fixture

Tasks 01 through 04 form the first four-item wave. Task 05 can begin only
after every prerequisite is integrated and reachable from the newest head.
