// Package testinfra provides throwaway infrastructure for integration tests:
// a Postgres container, a Kafka container, and the helpers needed to point a
// service's handlers at them. Each integration suite calls Start once and
// registers Close with t.Cleanup, so every suite gets its own isolated
// Postgres and Kafka and can run in parallel.
package testinfra

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/nedo/TicketSaas/pkg/kafka"
)

// Topics are every cross-service topic a suite may need to produce to or
// consume from. Created up front so consumers do not race topic creation.
var Topics = []string{
	"event.cancelled",
	"ticket.issued",
	"payment.completed",
	"payment.expired",
}

// smokeTopic is only used to confirm the broker accepts a produce.
const smokeTopic = "test-smoke"

const clusterID = "5L6g3nShT-eMCtK--X86sw"

// Infra holds the running containers plus the connections a suite needs.
// Producer and Brokers are nil when the suite was started with WithoutKafka.
type Infra struct {
	Pool     *pgxpool.Pool
	Brokers  []string
	Producer *kafka.Producer

	pg    testcontainers.Container
	kafka testcontainers.Container
}

type config struct {
	withKafka bool
}

// Option customises what Start boots.
type Option func(*config)

// WithoutKafka skips the Kafka container, for services that only use Postgres.
func WithoutKafka() Option {
	return func(c *config) { c.withKafka = false }
}

// Start boots Postgres with a database named "<service>_db", applies every
// *.up.sql migration for that service, boots Kafka unless WithoutKafka is
// given, creates Topics, and smoke-tests the broker. The returned Infra must be
// Closed.
func Start(ctx context.Context, service string, opts ...Option) (*Infra, error) {
	cfg := config{withKafka: true}
	for _, opt := range opts {
		opt(&cfg)
	}

	migrationsDir, err := MigrationsDir(service)
	if err != nil {
		return nil, err
	}

	dsn, cleanup, err := startPostgres(ctx, service)
	if err != nil {
		return nil, err
	}
	i := &Infra{pg: cleanup}

	pool, err := openPool(ctx, dsn)
	if err != nil {
		i.Close(ctx)
		return nil, err
	}
	i.Pool = pool

	if err := Migrate(ctx, pool, migrationsDir); err != nil {
		i.Close(ctx)
		return nil, err
	}

	if !cfg.withKafka {
		return i, nil
	}

	brokers, kc, err := startKafka(ctx)
	if err != nil {
		i.Close(ctx)
		return nil, err
	}
	i.kafka = kc
	i.Brokers = brokers

	topics := append(append([]string{}, Topics...), smokeTopic)
	for attempt := 1; ; attempt++ {
		if err = kafka.EnsureTopics(brokers, topics, 1, 1); err == nil {
			break
		}
		if attempt == 15 {
			i.Close(ctx)
			return nil, fmt.Errorf("kafka: ensure topics failed after 15 retries: %w", err)
		}
		time.Sleep(2 * time.Second)
	}

	i.Producer = kafka.NewProducer(brokers)

	// The first produce to a topic can block for seconds on a fresh broker.
	for attempt := 1; ; attempt++ {
		err = i.Producer.Produce(ctx, smokeTopic, "healthcheck", map[string]string{"ping": "pong"})
		if err == nil {
			break
		}
		if attempt == 20 {
			i.Close(ctx)
			return nil, fmt.Errorf("kafka: smoke test failed after 20 retries: %w", err)
		}
		time.Sleep(time.Second)
	}

	return i, nil
}

// MigrationsDir walks up from the working directory until it finds the
// repository's migrations/<service> directory, so suites do not have to hardcode
// how deep they sit under the repository root.
func MigrationsDir(service string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	for {
		candidate := filepath.Join(dir, "migrations", service)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("migrations dir for %q not found above %s", service, dir)
		}
		dir = parent
	}
}

// Close terminates both containers and releases the pool and producer.
// Safe to call on a partially constructed Infra.
func (i *Infra) Close(ctx context.Context) {
	if i.Producer != nil {
		_ = i.Producer.Close()
	}
	if i.Pool != nil {
		i.Pool.Close()
	}
	if i.kafka != nil {
		_ = i.kafka.Terminate(ctx)
	}
	if i.pg != nil {
		_ = i.pg.Terminate(ctx)
	}
}

// Migrate applies every *.up.sql file in dir in lexical order, which is how
// the numbered migrations are meant to be ordered.
func Migrate(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		sql, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply %s: %w\n%s", f, err, sql)
		}
	}
	return nil
}

// startPostgres boots the container and returns a DSN for the service database.
func startPostgres(ctx context.Context, service string) (string, testcontainers.Container, error) {
	c, err := tcpostgres.Run(
		ctx, "postgres:16-alpine",
		tcpostgres.WithUsername("ticketsaas"),
		tcpostgres.WithPassword("ticketsaas"),
		tcpostgres.WithDatabase(service+"_db"),
	)
	if err != nil {
		return "", nil, fmt.Errorf("postgres: %w", err)
	}

	// Let postgres settle before the first connection attempt.
	time.Sleep(2 * time.Second)

	host, err := c.Host(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("postgres host: %w", err)
	}
	port, err := c.MappedPort(ctx, "5432")
	if err != nil {
		return "", nil, fmt.Errorf("postgres port: %w", err)
	}

	dsn := fmt.Sprintf("postgres://ticketsaas:ticketsaas@%s:%s/%s_db?sslmode=disable", host, port.Port(), service)
	return dsn, c, nil
}

// openPool connects with a retry loop; a fresh container refuses the first
// connections while it finishes recovery.
func openPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	var err error
	for attempt := 1; attempt <= 10; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Second)
		}
		var pool *pgxpool.Pool
		if pool, err = pgxpool.New(ctx, dsn); err != nil {
			continue
		}
		if err = pool.Ping(ctx); err == nil {
			return pool, nil
		}
		pool.Close()
	}
	return nil, fmt.Errorf("connect after retries: %w", err)
}

func startKafka(ctx context.Context) ([]string, testcontainers.Container, error) {
	c, err := tckafka.Run(
		ctx, "confluentinc/cp-kafka:7.7.0",
		testcontainers.WithEnv(map[string]string{"CLUSTER_ID": clusterID}),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("kafka: %w", err)
	}
	brokers, err := c.Brokers(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("kafka brokers: %w", err)
	}
	return []string{brokers[0]}, c, nil
}
