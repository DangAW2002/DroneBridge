package mavlink_custom

import (
	"github.com/bluenviron/gomavlib/v3/pkg/dialect"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
)

// MessageSessionHeartbeat is a custom MAVLink message for session token synchronization
// Message ID: 42000
type MessageSessionHeartbeat struct {
	Token     [32]byte // Session token (32 bytes binary)
	ExpiresAt uint32   // Session expiration timestamp (Unix time)
	Sequence  uint16   // Sequence number for tracking
}

// GetID implements the Message interface
func (*MessageSessionHeartbeat) GetID() uint32 {
	return 42000
}

// GetCombinedDialect creates a dialect that includes both common and custom messages
func GetCombinedDialect() *dialect.Dialect {
	// Create new dialect based on common
	customDialect := &dialect.Dialect{
		Version:  common.Dialect.Version,
		Messages: append(common.Dialect.Messages, &MessageSessionHeartbeat{}),
	}
	return customDialect
}
