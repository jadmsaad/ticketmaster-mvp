This is a learning project. Read `PLAN.md` at the start of each session and pick up from its current status.

Work one task at a time: agree on the approach with the user before writing any code. At the end of each session, update the status, task checklist, and session log in `PLAN.md`.

Work on a branch, never on `main`. `main` is protected: a change lands as a pull request that merges when CI is green. Use one branch per task, named after it (`e2-t2-booking-service`, `chore/pr-workflow`). Open the PR with `gh pr create`; its body says why in a sentence or two, and lists the smaller decisions made in the task. Ask the user before pushing or opening the PR, and again before merging. Merge with squash.
