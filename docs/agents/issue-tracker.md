# Issue tracker: GitHub

Issues and specs for this repository live in GitHub Issues at https://github.com/COROTANjayson/travel-planner-api/issues. Use the GitHub CLI (`gh`) with `--repo COROTANjayson/travel-planner-api` explicitly, including when the remote uses an SSH host alias.

Run operations only for the work authorized by the user.

## Issue operations

- Create: `gh issue create --repo COROTANjayson/travel-planner-api --title "..." --body-file <file>`.
- Read: `gh issue view <number> --repo COROTANjayson/travel-planner-api --json number,title,body,labels,comments`.
- List: `gh issue list --repo COROTANjayson/travel-planner-api --state open --json number,title,body,labels`. Add label/state filters as needed; use `--limit` or pagination for exhaustive work.
- Comment: `gh issue comment <number> --repo COROTANjayson/travel-planner-api --body-file <file>`.
- Apply/remove labels: `gh issue edit <number> --repo COROTANjayson/travel-planner-api --add-label "..." --remove-label "..."`.
- Close: `gh issue close <number> --repo COROTANjayson/travel-planner-api`.

Put multiline bodies in a temporary UTF-8 file and pass `--body-file`.

"Publish to the issue tracker" means create a GitHub issue here. "Fetch the relevant ticket" means read that issue.

## Pull requests as a triage surface

**PRs as a request surface: no.**

Issue and pull request numbers share a namespace. Resolve the item type before changing it.

## Wayfinding

The map is an issue labelled `wayfinder:map`. Child tickets use `wayfinder:research`, `wayfinder:prototype`, `wayfinder:grilling`, or `wayfinder:task`.

Use native GitHub sub-issues and issue dependencies when available. Obtain numeric database IDs with `gh api repos/COROTANjayson/travel-planner-api/issues/<number> --jq .id`.

- Add a sub-issue: `gh api --method POST repos/COROTANjayson/travel-planner-api/issues/<parent>/sub_issues -F sub_issue_id=<child-database-id>`.
- Add a blocker: `gh api --method POST repos/COROTANjayson/travel-planner-api/issues/<child>/dependencies/blocked_by -F issue_id=<blocker-database-id>`.

If these features are unavailable, put `Part of #<parent>` and `Blocked by: #<number>` references in child bodies and list children as tasks in the map body.

Choose the first open child in map order that has no assignee or open blockers. Claim it with `gh issue edit <number> --repo COROTANjayson/travel-planner-api --add-assignee @me`. Resolve it by commenting with the answer, closing it, and recording the result and link in the map.

Create missing wayfinding labels only when wayfinding is requested.
