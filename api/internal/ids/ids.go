// Package ids creates primary keys. Version 7 UUIDs sort by creation time, which keeps indexes tidy.
package ids

import "github.com/google/uuid"

func New() uuid.UUID { return uuid.Must(uuid.NewV7()) }
