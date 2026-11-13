package storage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestOpenRejectsFilePersistence(t *testing.T) {
	for _, dsn := range []string{"", " ", filepath.Join(t.TempDir(), "data.json")} {
		s, err := Open(dsn)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("Open(%q) = %v, %v; want ErrInvalid", dsn, s, err)
		}
	}
}

func TestOpenRedactsInvalidDSN(t *testing.T) {
	for _, dsn := range []string{
		"postgres://postgres:secret-password@localhost:invalid/postgres",
		"host=localhost password=secret-password connect_timeout=invalid",
		"postgres://postgres:secret-password%zz@localhost/postgres",
	} {
		_, err := Open(dsn)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("want invalid DSN error, got %v", err)
		}
		if strings.Contains(err.Error(), "secret-password") {
			t.Fatal("invalid DSN error disclosed credentials")
		}
	}
}

func TestOperationsBoundDatabaseLocks(t *testing.T) {
	for _, name := range []string{"get", "records", "put", "delete", "replace"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, dsn := openTestStore(t)
			if _, err := s.Put("k", json.RawMessage(`1`), 0); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			c, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close(context.Background())
			tx, err := c.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err = tx.Exec(ctx, `LOCK TABLE records IN ACCESS EXCLUSIVE MODE`); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				var err error
				switch name {
				case "get":
					_, err = s.Get("k")
				case "records":
					_, err = s.Records()
				case "put":
					_, err = s.Put("k", json.RawMessage(`2`), 1)
				case "delete":
					_, err = s.Delete("k", 1)
				case "replace":
					err = s.ReplaceRecords(nil)
				}
				done <- err
			}()
			select {
			case err = <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("want deadline exceeded, got %v", err)
				}
			case <-time.After(6 * time.Second):
				t.Error("operation did not return within its deadline")
				if err := tx.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				<-done
			}
			if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Fatal(err)
			}
			if _, err := s.Get("k"); err != nil {
				t.Fatalf("pool unusable after cancellation: %v", err)
			}
		})
	}
}
