# Phase 5 Skills

Date: 2026-05-15

## Goal

Make reusable coding knowledge discoverable and auditable before it affects planner or worker behavior.

Skills are context files, not executable plugins. Phase 5 does not run skill code.

## Discovery Rules

WeazlCode discovers skills from configured paths in order:

1. `.weazlcode/skills`
2. `.agents/skills`
3. global Codex skills
4. global WeazlCode skills

Each skill is a directory containing `SKILL.md`. The skill name and description come from simple `name:` and `description:` headers, falling back to the directory name and the first body text when needed.

If two paths define the same skill name, the first configured path wins. This keeps project-local skills ahead of shared global skills.

## Planner Behavior

`/plan generate` includes a discovered skill catalog in orchestrator context. The planner can attach relevant skills to individual tasks by writing exact skill names into the task `skills` array.

If no skill is directly relevant, the planner should leave `skills` empty.

## Manual Attachment

Users can attach skills explicitly before approval:

```sh
/plan edit 1 skills go-tests,repo-style
```

Skills are stored on the task, shown in plan/task views, and persisted with the plan.

## Worker Packet Behavior

When a task has attached skills, WeazlCode loads the selected `SKILL.md` content and includes it in the worker packet under `skills`.

Unknown selected skills fail packet generation instead of silently being ignored.

## Safety Rules

- Skills are read-only context.
- Skills are never executed.
- Project-local skills have precedence over global skills.
- Worker packets include only explicitly attached skills.
- Skill content is bounded before being placed into a packet.

## Smoke Criteria

- `/skills` lists discovered skills with source path and description.
- Generated planner prompt includes the skill catalog.
- `/plan edit <task> skills <names>` persists task skill attachments.
- `/packet` includes selected skill content for known skills.
- Unknown skill names block worker packet generation with a clear error.
