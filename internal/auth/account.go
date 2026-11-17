package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/PazVz/mini_coder/internal/database/dbgen"
)

// beginAccountChange serializes sensitive account writes with login and administrator
// changes, then rechecks the current session after any wait for the user row lock.
func (s *Service) beginAccountChange(ctx context.Context, userID int64, token string) (pgx.Tx, *dbgen.Queries, dbgen.LockAuthUserByIDRow, error) {
	var user dbgen.LockAuthUserByIDRow
	if !validToken(token) {
		return nil, nil, user, ErrUnauthenticated
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, user, fmt.Errorf("begin account change: %w", err)
	}
	queries := s.queries.WithTx(tx)
	user, err = queries.LockAuthUserByID(ctx, userID)
	if err == nil {
		var session dbgen.GetSessionUserRow
		session, err = queries.GetSessionUser(ctx, tokenHash(token))
		if err == nil && session.ID != user.ID {
			err = ErrUnauthenticated
		}
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, user, ErrUnauthenticated
		}
		return nil, nil, user, fmt.Errorf("authenticate account change: %w", err)
	}
	return tx, queries, user, nil
}
