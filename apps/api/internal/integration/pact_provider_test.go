//go:build pact

// Provider-side verification of the pact JSON the web consumer publishes.
//
// Pipeline:
//
//   1. Web's `vitest run --config vitest.pact.config.ts` writes
//      `apps/web/pacts/visionloop-web-visionloop-api.json`.
//   2. This test boots Postgres + RabbitMQ via testcontainers (same
//      helpers as the e2e test), wires the production router, wraps it
//      in httptest.Server, and runs `pact-go` v2's verifier against the
//      pact file.
//   3. State handlers seed the DB rows each interaction expects.
//
// Auth: the verifier injects a `better-auth.session_token=valid-token`
// cookie via a request filter so the constSessions stub can resolve it.
//
// Run: `make test-pact-provider`. Requires `libpact_ffi.so` on the
// loader path — installed by `pact-go install --libDir /tmp/pact-libs`,
// driven from the Makefile target.
package integration_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pact-foundation/pact-go/v2/models"
	"github.com/pact-foundation/pact-go/v2/provider"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/infrastructure/eventbus"
	"github.com/visionloop/api/internal/infrastructure/queue"
	"github.com/visionloop/api/internal/infrastructure/repo"
	httpapi "github.com/visionloop/api/internal/interfaces/http"
)

// fixedAnnotatorID lets state handlers and the constUsers stub agree on
// which user the verifier authenticates as. The pact file pins specific
// uuids in some interactions (e.g. the confirm path); we map the
// "caller" persona to that user across all states.
const (
	fixedSessionToken = "valid-token"
	fixedImageID      = "00000000-0000-0000-0000-0000000000a1"
)

func TestPactProvider_VerifyConsumerContracts(t *testing.T) {
	if _, err := os.Stat(pactFilePath(t)); err != nil {
		t.Fatalf("pact file missing — run `bun run test:pact:consumer` in apps/web first: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	pool := startPostgres(ctx, t)
	rabbitURL := startRabbit(ctx, t)
	mq, err := queue.Dial(ctx, rabbitURL, 10*time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("dial rabbit: %v", err)
	}
	t.Cleanup(func() { _ = mq.Close() })

	// One persona: an annotator in a fresh org. State handlers below
	// flip the role / pre-seed images as each interaction requires.
	orgID, userID := uuid.New(), uuid.New()
	insertOrgUserMembership(t, pool, orgID, userID, domain.RoleAnnotator)

	role := mutableRole{value: domain.RoleAnnotator}
	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger:                slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Pool:                  pool,
		Sessions:              constSessions{userID: userID},
		Users:                 dynamicUsers{userID: userID, orgID: orgID, role: &role},
		ImagesRepo:            repo.NewImageRepo(pool),
		JobsRepo:              repo.NewJobRepo(pool),
		JobPublisher:          queue.NewJobPublisherAdapter(queue.NewAMQPPublisher(mq)),
		JobEvents:             eventbus.NewMemoryHub(),
		AnnotationsRepo:       repo.NewAnnotationRepo(pool),
		AnnotationSetsRepo:    repo.NewAnnotationRepo(pool),
		Storage:               fakeStorage{},
		Audit:                 repo.NewAuditRecorder(pool),
		WebOrigin:             "http://localhost:3000",
		PresignTTL:            15 * time.Minute,
		StartedAt:             time.Now(),
		JobCallbackToken:      "pact-callback-token-of-at-least-32-bytes-long",
		JobCallbackHMACSecret: "pact-callback-hmac-of-at-least-32-bytes-padding",
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	v := provider.NewVerifier()
	err = v.VerifyProvider(t, provider.VerifyRequest{
		ProviderBaseURL: srv.URL,
		PactFiles:       []string{pactFilePath(t)},
		Provider:        "visionloop-api",
		// Cookie + Accept header that the production web client always
		// sends. The consumer pact does not assert these explicitly so
		// we add them at the verifier layer.
		RequestFilter: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: fixedSessionToken})
				if r.Header.Get("Accept") == "" {
					r.Header.Set("Accept", "application/json")
				}
				next.ServeHTTP(w, r)
			})
		},
		StateHandlers: models.StateHandlers{
			"a session for an owner of an organisation": func(setup bool, _ models.ProviderState) (models.ProviderStateResponse, error) {
				if setup {
					setRole(t, pool, orgID, userID, domain.RoleOwner)
					role.value = domain.RoleOwner
				}
				return nil, nil
			},
			"an org with no images": func(setup bool, _ models.ProviderState) (models.ProviderStateResponse, error) {
				if setup {
					if _, err := pool.Exec(context.Background(), `DELETE FROM images WHERE org_id = $1`, orgID); err != nil {
						return nil, err
					}
				}
				return nil, nil
			},
			"a session for an annotator with quota for one image": func(setup bool, _ models.ProviderState) (models.ProviderStateResponse, error) {
				if setup {
					setRole(t, pool, orgID, userID, domain.RoleAnnotator)
					role.value = domain.RoleAnnotator
				}
				return nil, nil
			},
			"an image in uploading state owned by the caller": func(setup bool, _ models.ProviderState) (models.ProviderStateResponse, error) {
				if setup {
					setRole(t, pool, orgID, userID, domain.RoleAnnotator)
					role.value = domain.RoleAnnotator
					imgID := uuid.MustParse(fixedImageID)
					_, err := pool.Exec(context.Background(),
						`INSERT INTO images (id, org_id, uploaded_by, storage_key, content_type, byte_size, status)
						 VALUES ($1, $2, $3, $4, 'image/jpeg', 1024, 'uploading')
						 ON CONFLICT (id) DO UPDATE
						   SET status = 'uploading', org_id = EXCLUDED.org_id, uploaded_by = EXCLUDED.uploaded_by`,
						imgID, orgID, userID,
						"orgs/"+orgID.String()+"/images/"+imgID.String()+".jpg")
					if err != nil {
						return nil, err
					}
				}
				return nil, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("pact verification failed: %v", err)
	}
}

// pactFilePath resolves apps/web/pacts/<consumer>-<provider>.json
// relative to this test file. Returning a stable path means the test
// fails fast (with guidance) when the consumer side has not run.
func pactFilePath(t *testing.T) string {
	t.Helper()
	_, this, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(this), "..", "..", "..", "..",
		"apps", "web", "pacts", "visionloop-web-visionloop-api.json")
}

// insertOrgUserMembership / setRole keep state-handler bodies short.
// Each handler only flips the bits an interaction needs.
func insertOrgUserMembership(t *testing.T, pool *pgxpool.Pool, orgID, userID uuid.UUID, role domain.Role) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO organizations (id, slug, name, plan, cors_allowlist) VALUES ($1, $2, 'Pact', 'free', '{}')`,
		orgID, "pact-"+orgID.String()[:8]); err != nil {
		t.Fatalf("org: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, email_verified, display_name, locale) VALUES ($1, $2, true, 'Pact', 'en')`,
		userID, userID.String()+"@pact"); err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, $3)`, orgID, userID, role); err != nil {
		t.Fatalf("membership: %v", err)
	}
}

func setRole(t *testing.T, pool *pgxpool.Pool, orgID, userID uuid.UUID, role domain.Role) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE memberships SET role = $3 WHERE org_id = $1 AND user_id = $2`,
		orgID, userID, role); err != nil {
		t.Fatalf("set role: %v", err)
	}
}

// mutableRole + dynamicUsers let the state handler change the caller's
// role without rebuilding the router. constUsers is read-only so the
// e2e test gets a stable persona; pact verification needs it to flip.
type mutableRole struct{ value domain.Role }

type dynamicUsers struct {
	userID uuid.UUID
	orgID  uuid.UUID
	role   *mutableRole
}

func (d dynamicUsers) GetUser(_ context.Context, _ uuid.UUID) (domain.User, error) {
	return domain.User{ID: d.userID, Email: "owner@example.com", DisplayName: "Owner", EmailVerified: true, Locale: "en"}, nil
}
func (d dynamicUsers) PrimaryMembership(_ context.Context, _ uuid.UUID) (domain.Membership, error) {
	return domain.Membership{OrgID: d.orgID, UserID: d.userID, Role: d.role.value}, nil
}

// fakeStorage returns deterministic presigned URLs without hitting MinIO.
// The pact contract doesn't constrain the URL beyond a regex, so any
// non-empty string suffices.
type fakeStorage struct{}

func (fakeStorage) PresignPut(_ context.Context, key, contentType string, _ int64, ttl time.Duration) (application.PresignedURL, error) {
	return application.PresignedURL{
		URL:     "http://localhost:9000/visionloop/" + key + "?X-Amz-Signature=test",
		Method:  "PUT",
		Headers: map[string]string{"Content-Type": contentType},
		Expires: time.Now().Add(ttl),
	}, nil
}

func (fakeStorage) HeadObject(_ context.Context, _ string) (application.ObjectInfo, error) {
	return application.ObjectInfo{ByteSize: 1024, ContentType: "image/jpeg"}, nil
}
