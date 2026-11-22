package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// genesis is the "previous hash" of the first entry of every chain.
var genesis = make([]byte, sha256.Size)

// row is one stored entry, in the shape that gets hashed.
type row struct {
	ID          uuid.UUID
	Org         uuid.UUID
	Seq         int64
	At          time.Time
	Actor       *uuid.UUID
	Role        string
	ViewAs      string
	Property    *uuid.UUID
	Action      string
	TargetType  string
	Target      *uuid.UUID
	TargetLabel string
	Before      []byte
	After       []byte
	Reason      string
	Device      string
	IP          string
	Trace       *uuid.UUID
	Prev        []byte
	Hash        []byte
}

// canonical is the fixed form of a row that gets hashed. Field order is the struct order, and JSON values
// are re-encoded with sorted keys, so the same row always yields the same bytes.
type canonical struct {
	ID          uuid.UUID       `json:"id"`
	Org         uuid.UUID       `json:"org"`
	Seq         int64           `json:"seq"`
	At          string          `json:"at"`
	Actor       *uuid.UUID      `json:"actor"`
	Role        string          `json:"role"`
	ViewAs      string          `json:"viewAs"`
	Property    *uuid.UUID      `json:"property"`
	Action      string          `json:"action"`
	TargetType  string          `json:"targetType"`
	Target      *uuid.UUID      `json:"target"`
	TargetLabel string          `json:"targetLabel"`
	Before      json.RawMessage `json:"before"`
	After       json.RawMessage `json:"after"`
	Reason      string          `json:"reason"`
	Device      string          `json:"device"`
	IP          string          `json:"ip"`
	Trace       *uuid.UUID      `json:"trace"`
}

func (r row) hash() ([]byte, error) {
	before, err := sortedJSON(r.Before)
	if err != nil {
		return nil, err
	}
	after, err := sortedJSON(r.After)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(canonical{r.ID, r.Org, r.Seq, r.At.UTC().Format(time.RFC3339Nano), r.Actor, r.Role, r.ViewAs, r.Property, r.Action,
		r.TargetType, r.Target, r.TargetLabel, before, after, r.Reason, r.Device, r.IP, r.Trace})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(append(append([]byte{}, r.Prev...), body...))
	return sum[:], nil
}

// sortedJSON re-encodes JSON so object keys are sorted (a Go map marshals in key order). Empty input is null.
func sortedJSON(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("null"), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// secretKeys are dropped from Before and After, at any depth, so a password, code or token never reaches the trail.
var secretKeys = []string{"password", "code", "token", "secret"}

func isSecret(key string) bool {
	k := strings.ToLower(key)
	for _, s := range secretKeys {
		if k == s || strings.HasSuffix(k, "_"+s) || strings.HasSuffix(k, s+"hash") {
			return true
		}
	}
	return false
}

func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if !isSecret(k) {
				out[k] = redact(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = redact(val)
		}
		return out
	default:
		return v
	}
}

// redactedJSON turns a value into JSON with the secret keys removed. A nil value stays nil.
func redactedJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v) // round trip through JSON so structs and maps are treated alike
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(redact(generic))
}

// VerifyChain recomputes every hash of one organization in order. It returns the ID of the first entry that
// does not fit (edited, or following a removed entry), or nil when the chain is whole.
// Run it inside db.WithOrg for that organization.
func VerifyChain(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (brokenAt *uuid.UUID, err error) {
	rows, err := tx.Query(ctx,
		`select id, seq, occurred_at, actor_user_id, actor_role, view_as, property_id, action, target_type, target_id, target_label,
		        before::text, after::text, reason, device, ip, trace_id, prev_hash, row_hash
		 from audit_log where organization_id = $1 order by seq`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	want := genesis
	for rows.Next() {
		r := row{Org: orgID}
		var before, after *string
		if err := rows.Scan(&r.ID, &r.Seq, &r.At, &r.Actor, &r.Role, &r.ViewAs, &r.Property, &r.Action, &r.TargetType, &r.Target, &r.TargetLabel,
			&before, &after, &r.Reason, &r.Device, &r.IP, &r.Trace, &r.Prev, &r.Hash); err != nil {
			return nil, err
		}
		if before != nil {
			r.Before = []byte(*before)
		}
		if after != nil {
			r.After = []byte(*after)
		}
		sum, err := r.hash()
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(r.Prev, want) || !bytes.Equal(r.Hash, sum) {
			id := r.ID
			return &id, nil
		}
		want = r.Hash
	}
	return nil, rows.Err()
}
