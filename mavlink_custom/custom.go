package mavlink_custom

import (
	"github.com/bluenviron/gomavlib/v3/pkg/dialect"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/all"
	"github.com/bluenviron/gomavlib/v3/pkg/message"
)

// MessageSessionHeartbeat is a custom MAVLink message for session token synchronization
// Message ID: 42999 (Changed from 42000 to avoid conflicts)
type MessageSessionHeartbeat struct {
	Token     [32]byte // Session token (32 bytes binary)
	ExpiresAt uint32   // Session expiration timestamp (Unix time)
	Sequence  uint16   // Sequence number for tracking
}

// GetID implements the Message interface
func (*MessageSessionHeartbeat) GetID() uint32 {
	return 42999
}

// GetCombinedDialect creates a dialect that includes both all standard and custom messages
func GetCombinedDialect() *dialect.Dialect {
	// First, check if our ID is already in all.Dialect (extremely unlikely for 42999)
	for _, msg := range all.Dialect.Messages {
		if msg.GetID() == 42999 {
			return all.Dialect // Already exists, just return all
		}
	}

	// Create a NEW slice to avoid modifying the original all.Dialect global slice
	allMsgs := make([]message.Message, len(all.Dialect.Messages))
	copy(allMsgs, all.Dialect.Messages)
	allMsgs = append(allMsgs, &MessageSessionHeartbeat{})

	customDialect := &dialect.Dialect{
		Version:  all.Dialect.Version,
		Messages: allMsgs,
	}
	return customDialect
}
