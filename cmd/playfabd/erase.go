package main

import "context"

// erase removes every title-player row of the OpenPak account (card openpak/prds#120): the
// PlayFab id and entity id minted for it on each title. Idempotent; other accounts are untouched.
// The PlayFab session tokens already issued are HMAC credentials that carry the ids, are never
// stored, and stop being accepted because the row they are checked against is gone.
func (s *store) erase(ctx context.Context, account string) (int, error) {
	t, err := s.pool.Exec(ctx, `DELETE FROM playfab_players WHERE lower(account_id) = lower($1)`, account)
	if err != nil {
		return 0, err
	}
	return int(t.RowsAffected()), nil
}
