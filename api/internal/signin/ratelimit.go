package signin

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	attemptWindow = 15 * time.Minute
	maxPerAccount = 10
	maxPerIP      = 50
)

func accountKey(email string) string {
	if len(email) > 254 {
		email = email[:254]
	}
	return "acct:" + email
}

// clientIP is the address of the connection. Behind a reverse proxy this would be the proxy;
// reading a forwarded header safely needs a trusted-proxy setting, which is not built yet.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return "ip:" + host
}

func failures(ctx context.Context, tx pgx.Tx, key string, now time.Time) (n int, err error) {
	err = tx.QueryRow(ctx, `select count(*) from signin_attempt where key = $1 and at > $2`, key, now.Add(-attemptWindow)).Scan(&n)
	return
}

// locked reports whether either key has used up its failures inside the window.
func locked(ctx context.Context, tx pgx.Tx, acctKey, ipKey string, now time.Time) (bool, error) {
	a, err := failures(ctx, tx, acctKey, now)
	if err != nil || a >= maxPerAccount {
		return a >= maxPerAccount, err
	}
	i, err := failures(ctx, tx, ipKey, now)
	return i >= maxPerIP, err
}

func recordFailure(ctx context.Context, tx pgx.Tx, now time.Time, keys ...string) error {
	for _, k := range keys {
		if _, err := tx.Exec(ctx, `delete from signin_attempt where key = $1 and at <= $2`, k, now.Add(-time.Hour)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into signin_attempt (key, at) values ($1, $2)`, k, now); err != nil {
			return err
		}
	}
	return nil
}
