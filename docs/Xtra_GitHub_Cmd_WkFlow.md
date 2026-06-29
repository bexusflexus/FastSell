Extra GitHub Command Reference


# GitHub Command Reference

This is a practical Git and GitHub CLI reference for working on FastSell.

The project is a public repository. Before pushing any branch, inspect the diff for private paths, hostnames, personal usernames, secrets, `.env` values, tokens, keys, local machine names, and test data that should not be public.

## 1. Check repository status

Use this before and after almost every change.

```
git status
```

Shows:

- Current branch
- Modified files
- Staged files
- Untracked files
- Whether branch is ahead/behind remote

Useful before:

- Creating a branch
- Adding files
- Committing
- Pushing
- Opening a pull request

## 2. Show current branch

```
git branch --show-current
```

Use this before making changes to confirm you are not working on `main` by accident.

Example:

```
git branch --show-current
```

Expected:

```
17-fix-installer-ownership-model
```

## 3. Update local main

Use this before creating a new feature branch.

```
git switch main
git pull --ff-only
```

What it does:

- Switches to main
- Pulls the latest remote changes
- Refuses to merge if local main has diverged

Use `--ff-only` to avoid accidental merge commits.

## 4. Create a new feature branch

```
git switch -c 17-fix-installer-ownership-model
```

What it does:

- Creates a new branch
- Switches to it immediately

Use for new work items, bug fixes, docs changes, or installer changes.

Branch naming pattern:

```
<issue-number>-short-description
```

Examples:

```
git switch -c 17-fix-installer-ownership-model
git switch -c 18-update-system-requirements
git switch -c 19-fix-upload-draft-restore
```

## 5. Switch to an existing branch

```
git switch 17-fix-installer-ownership-model
```

Use when returning to a branch that already exists locally.

To list local branches:

```
git branch
```

To list local and remote branches:

```
git branch -a
```

## 6. Inspect changed files

```
git diff --stat
```

Shows a compact file-by-file summary of changes.

Use before reading the full diff.

Example:

```
git diff --stat
```

## 7. Inspect full unstaged changes

```
git diff
```

Shows exact unstaged changes. Use this before staging files.

For a specific file:

```
git diff deploy/linux/install.sh
```

## 8. Inspect staged changes

```
git diff --staged
```

Shows what will be committed. Use this immediately before `git commit`.

## 9. Search repo for risky or relevant text

Use `rg` instead of `grep`.

Search for installer ownership terms:

```
rg -n '956|FASTSELL_UID|FASTSELL_GID|root:fastsell|groupadd --system fastsell|useradd.*fastsell|chown .*fastsell|APP_UID|APP_GID'
```

Use this while fixing installer ownership logic.

Search for private/local details before pushing:

```
rg -n --hidden \
  --glob '!.git' \
  --glob '!node_modules' \
  --glob '!frontend/node_modules' \
  --glob '!dist' \
  --glob '!build' \
  'localhost|\.env|password|secret|token|api_key|apikey|ssh|hostname|home/|/Users/|/mnt/|/run/media'
```

This is not perfect, but it catches common leaks.

Search for installed FastSell paths:

```
rg -n '/srv/fastsell|/opt/fastsell|FASTSELL_ENV_FILE|LISTING_PHOTO_EXPORT_HOST_ROOT'
```

Use when working on installer docs or runtime path behavior.

## 10. Stage changes

Stage one file:

```
git add deploy/linux/install.sh
```

Stage multiple known files:

```
git add \
  deploy/linux/install.sh \
  deploy/linux/update.sh \
  scripts/usb/install-fastsell-debian-from-usb.sh \
  scripts/usb/update.sh \
  docker-compose.yml
```

Stage all changes:

```
git add .
```

Prefer staging specific files when working in a public repo so accidental files are not included.

## 11. Unstage a file

```
git restore --staged path/to/file
```

Example:

```
git restore --staged /tmp/test-output.txt
```

Use when a file was staged by mistake.

## 12. Discard unstaged changes

Discard one file:

```
git restore path/to/file
```

Discard all unstaged changes:

```
git restore .
```

Use carefully. This throws away local edits.

## 13. Commit changes

```
git commit -m "Fix installer ownership model"
```

Good commit messages are short and specific.

Examples:

```
git commit -m "Fix installer ownership model"
git commit -m "Document GitHub command workflow"
git commit -m "Update FastSell export permissions"
```

Before committing, run:

```
git status
git diff --staged
```

## 14. Push a branch

First push for a new branch:

```
git push -u origin 17-fix-installer-ownership-model
```

After the first push:

```
git push
```

Before pushing a public repo branch, run:

```
git status
git diff --staged
rg -n --hidden \
  --glob '!.git' \
  --glob '!node_modules' \
  --glob '!frontend/node_modules' \
  --glob '!dist' \
  --glob '!build' \
  'password|secret|token|api_key|apikey|PRIVATE KEY|\.env'
```

## 15. Create a GitHub issue with gh

Create issue interactively:

```
gh issue create
```

Create issue with title and body:

```
gh issue create \
  --title "Fix installer ownership model for app data and export folders" \
  --body "Clean Linux installs should not leave /srv/fastsell/data owned by orphaned numeric UID/GID values."
```

Create issue from a markdown file:

```
gh issue create \
  --title "Fix installer ownership model for app data and export folders" \
  --body-file /tmp/fastsell-ownership-issue.md
```

Use `--body-file` for longer issues.

## 16. List GitHub issues

```
gh issue list
```

Show open issues:

```
gh issue list --state open
```

Show issues assigned to you:

```
gh issue list --assignee @me
```

Search issue titles/body:

```
gh issue list --search "ownership"
```

## 17. View a GitHub issue

```
gh issue view 17
```

Open in browser:

```
gh issue view 17 --web
```

## 18. Check out an issue branch

If using GitHub issue numbers as branch names:

```
git switch -c 17-fix-installer-ownership-model
```

If a branch already exists remotely:

```
git fetch origin
git switch 17-fix-installer-ownership-model
```

## 19. Create a pull request

Use this after pushing a feature branch.

```
gh pr create \
  --base main \
  --head 17-fix-installer-ownership-model \
  --title "Fix installer ownership model" \
  --body "Fixes installer ownership so app data and export folders are root-owned and host-browsable while PostgreSQL data remains container-owned."
```

Open PR creation in browser:

```
gh pr create --web
```

## 20. View pull request status

```
gh pr status
```

View current branch PR:

```
gh pr view
```

Open current PR in browser:

```
gh pr view --web
```

## 21. Review CI/check status

```
gh pr checks
```

Use this after opening or updating a PR.

## 22. Add commits to an existing PR

Make changes, then:

```
git status
git diff
git add path/to/file
git commit -m "Address ownership validation output"
git push
```

The PR updates automatically after push.

## 23. Sync branch with latest main

Use this if `main` changed after your branch was created.

```
git switch main
git pull --ff-only
git switch 17-fix-installer-ownership-model
git rebase main
```

Then push the updated branch:

```
git push --force-with-lease
```

Use `--force-with-lease`, not plain `--force`.

## 24. Delete a local branch after merge

```
git switch main
git pull --ff-only
git branch -d 17-fix-installer-ownership-model
```

If Git refuses because the branch is not merged, stop and inspect before forcing.

After a squash merge, Git may not recognize the feature branch as merged. If the PR is definitely merged into `main`, delete the local branch with:

```
git branch -D 17-fix-installer-ownership-model
```

## 25. Delete a remote branch after merge

Usually GitHub can delete the branch from the PR page.

Command line:

```
git push origin --delete 17-fix-installer-ownership-model
```

## 26. Common issue workflow

Use this flow for most FastSell public repo work:

```
git switch main
git pull --ff-only
git switch -c 17-fix-installer-ownership-model

# make changes
git status
git diff --stat
git diff

# inspect for public repo safety
rg -n --hidden \
  --glob '!.git' \
  --glob '!node_modules' \
  --glob '!frontend/node_modules' \
  --glob '!dist' \
  --glob '!build' \
  'password|secret|token|api_key|apikey|PRIVATE KEY|\.env'

git add \
  deploy/linux/install.sh \
  deploy/linux/update.sh \
  scripts/usb/install-fastsell-debian-from-usb.sh \
  scripts/usb/update.sh \
  docker-compose.yml \
  docs/

git diff --staged
git commit -m "Fix installer ownership model"
git push -u origin 17-fix-installer-ownership-model

gh pr create \
  --base main \
  --head 17-fix-installer-ownership-model \
  --title "Fix installer ownership model" \
  --body "Fixes installer ownership so app data and export folders are host-browsable while PostgreSQL data remains container-owned."
```

## 27. Forgot to create a feature branch before editing main

Use this when you are sitting on `main` locally and already made edits before creating the feature branch.

This saves the work, cleans `main`, creates the feature branch, then applies the saved work to that branch.

```
git status --short
git branch --show-current

# save tracked and untracked edits, then clean main
git stash push -u -m "wip: start feature branch"

git status --short
git pull --ff-only

# create and switch to the feature branch
git switch -c 18-update-github-command-reference

# inspect and apply the saved work to the new branch
git stash list
git stash show --stat stash@{0}
git stash pop stash@{0}

git status --short
```

Notes:

- `git stash push -u` includes untracked files. Without `-u`, brand-new files may be left behind on `main`.
- A stash is repository-wide, not actually stored on `main`. A successful `git stash pop` applies the changes and removes that stash.
- Use `git stash apply` instead of `pop` only if you want to keep the stash as a backup until you manually drop it.

If `git stash pop` reports conflicts, resolve them on the feature branch, then continue:

```
# after fixing conflicts
git status --short
git add path/to/resolved-file
git commit -m "Start feature work"
```

If the stash is still listed after the conflict is resolved and committed, drop it manually:

```
git stash list
git stash drop stash@{0}
```

## 28. Return to main and delete a squash-merged feature branch

Use this after the PR has been merged into `main` and GitHub used squash merge.

Because squash merges create a new commit on `main`, Git may not recognize the feature branch as merged. That is why this workflow uses `-D` after you have confirmed the PR is merged.

```
git status --short
git switch main
git pull --ff-only
git branch -D 17-fix-installer-ownership-model
git push origin --delete 17-fix-installer-ownership-model
```

If the remote branch was already deleted from the GitHub PR page, the final command may report that the remote ref does not exist. That is harmless.

## 29. Useful gh setup checks

Check GitHub CLI authentication:

```
gh auth status
```

Login:

```
gh auth login
```

Set default protocol to SSH:

```
gh config set git_protocol ssh
```

View current config:

```
gh config list
```

## 30. Clone a repo with gh

```
gh repo clone OWNER/REPO
```

Example pattern:

```
gh repo clone example/FastSell-AssetMgmt
```

Use the real owner/repo name when cloning.

## 31. Public repo safety checklist before push

Run:

```
git status
git diff --staged
rg -n --hidden \
  --glob '!.git' \
  --glob '!node_modules' \
  --glob '!frontend/node_modules' \
  --glob '!dist' \
  --glob '!build' \
  'password|secret|token|api_key|apikey|PRIVATE KEY|BEGIN OPENSSH|BEGIN RSA|\.env'
```

Check for:

- Secrets
- Real `.env` values
- API keys
- Private hostnames
- Personal usernames
- Local-only mount paths
- Backups
- Test photos
- Generated files
- Database dumps

Do not push if anything private appears.
