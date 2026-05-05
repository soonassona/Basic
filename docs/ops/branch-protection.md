# Branch protection — `main`

Configure on GitHub: **Settings → Branches → Branch protection rules →
Add rule** with pattern `main`.

| Setting                                                | Value      |
| ------------------------------------------------------ | ---------- |
| Require a pull request before merging                  | ✅          |
| ↳ Require approvals                                    | 1          |
| ↳ Dismiss stale approvals on new commits               | ✅          |
| ↳ Require review from Code Owners                      | ✅          |
| Require status checks to pass before merging           | ✅          |
| ↳ Require branches to be up to date before merging     | ✅          |
| ↳ Required checks                                      | `web / lint-typecheck-test`, `web / docker-build`, `api / lint-typecheck-test`, `api / docker-build`, `ai-service / lint-typecheck-test`, `ai-service / docker-build`, `contracts / pact` |
| Require conversation resolution before merging         | ✅          |
| Require signed commits                                 | ✅          |
| Require linear history                                 | ✅          |
| Require deployments to succeed                         | ⛔ (handled by promotion gate, not merge) |
| Lock branch                                            | ⛔          |
| Do not allow bypassing the above settings              | ✅          |
| Allow force pushes                                     | ⛔          |
| Allow deletions                                        | ⛔          |

## Required GitHub repository secrets

| Secret                  | Purpose                                       |
| ----------------------- | --------------------------------------------- |
| `GHCR_TOKEN`            | Pushes images to `ghcr.io` on `main`          |
| `R2_ACCESS_KEY_ID`      | Staging R2 access key (CI integration tests)  |
| `R2_SECRET_ACCESS_KEY`  | Staging R2 secret                             |
| `BETTER_AUTH_SECRET`    | Shared between web and api in staging         |
| `HONEYCOMB_API_KEY`     | Production traces (Phase 6)                   |
| `PACT_BROKER_TOKEN`     | Contract publication                          |

Secrets are scoped to the `production` and `staging` environments where
possible. CI for pull requests from forks runs without these secrets and
skips the steps that require them.
