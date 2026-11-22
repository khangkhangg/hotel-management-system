package password

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // the breach list is keyed by SHA-1; this is not a security use
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

const rangeURL = "https://api.pwnedpasswords.com/range/"

type rangeChecker struct{ client *http.Client }

// NewRangeChecker asks the Have I Been Pwned range API. Only the first 5 characters of the
// password's SHA-1 leave this server, so the service never learns the password or its full hash.
func NewRangeChecker(client *http.Client) BreachChecker { return rangeChecker{client: client} }

func (c rangeChecker) Breached(ctx context.Context, plain string) (bool, error) {
	sum := sha1.Sum([]byte(plain)) //nolint:gosec
	full := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := full[:5], full[5:]

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rangeURL+prefix, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Add-Padding", "true") // the response length then does not reveal how many matches exist
	resp, err := c.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("breach list answered %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		hash, count, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(hash, suffix) {
			continue
		}
		return count != "0", nil // padding entries carry a count of 0
	}
	return false, sc.Err()
}
