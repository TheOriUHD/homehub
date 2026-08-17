package store

import (
	"context"

	"github.com/google/uuid"
)

// UserSecurityInfo aggregates the per-user facts the API decorates user
// objects with, fetched in bulk to avoid N+1 queries on list endpoints.
type UserSecurityInfo struct {
	RoleIDs      map[uuid.UUID][]uuid.UUID
	TOTPEnabled  map[uuid.UUID]bool
	PasskeyCount map[uuid.UUID]int
}

func (s *Store) LoadUserSecurityInfo(ctx context.Context) (*UserSecurityInfo, error) {
	info := &UserSecurityInfo{
		RoleIDs:      map[uuid.UUID][]uuid.UUID{},
		TOTPEnabled:  map[uuid.UUID]bool{},
		PasskeyCount: map[uuid.UUID]int{},
	}

	rows, err := s.pool.Query(ctx, `select user_id, role_id from user_roles`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u, r uuid.UUID
		if err := rows.Scan(&u, &r); err != nil {
			rows.Close()
			return nil, err
		}
		info.RoleIDs[u] = append(info.RoleIDs[u], r)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	rows, err = s.pool.Query(ctx, `select user_id from totp_credentials where confirmed_at is not null`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u uuid.UUID
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return nil, err
		}
		info.TOTPEnabled[u] = true
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	rows, err = s.pool.Query(ctx, `select user_id, count(*) from webauthn_credentials group by user_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u uuid.UUID
		var n int
		if err := rows.Scan(&u, &n); err != nil {
			rows.Close()
			return nil, err
		}
		info.PasskeyCount[u] = n
	}
	rows.Close()
	return info, rows.Err()
}

// UserSecurityInfoFor loads the same aggregates for a single user.
func (s *Store) UserSecurityInfoFor(ctx context.Context, userID uuid.UUID) (roleIDs []uuid.UUID, totpEnabled bool, passkeyCount int, err error) {
	roleIDs, err = s.UserRoleIDs(ctx, userID)
	if err != nil {
		return nil, false, 0, err
	}
	err = s.pool.QueryRow(ctx, `
		select
			exists (select 1 from totp_credentials where user_id = $1 and confirmed_at is not null),
			(select count(*) from webauthn_credentials where user_id = $1)`,
		userID).Scan(&totpEnabled, &passkeyCount)
	return roleIDs, totpEnabled, passkeyCount, err
}
